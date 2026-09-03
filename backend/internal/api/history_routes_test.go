package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestConsoleCommandRequestDetail(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	runtime := fixture.server.activeRuntime()
	requestID := insertRouteCommandRequest(t, fixture.db, token.ID, server.ID, "running")
	manualID := insertManualRouteCommandRequest(t, fixture.db, server.ID, "nano /etc/hosts ...", "interactive_editor")
	detailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/command-requests/"+strconv.FormatInt(manualID, 10), "", nil)
	if detailResponse.Code != http.StatusOK || !strings.Contains(detailResponse.Body.String(), `"source":"manual"`) || !strings.Contains(detailResponse.Body.String(), `"tracking_reason":"interactive_editor"`) {
		t.Fatalf("manual command request detail failed: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	if manualID < 1 {
		t.Fatalf("expected manual request id")
	}
	record, err := fixture.server.getCommandRequest(ctx, runtime, requestID, token.ID, commandRequestSourceMCP)
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	if record.Status != "running" || record.AssistantHint != runningCommandRequestAssistantHint {
		t.Fatalf("running command request should keep polling hint: %#v", record)
	}
}

func TestBulkConsoleCommandCreatesManualHistoryRows(t *testing.T) {
	fixture := newAPITestFixture(t)
	serverOne := fixture.createKeyAndServer(t, "bulk-one")
	serverTwo := fixture.createKeyAndServer(t, "bulk-two")
	if _, err := fixture.db.Exec(`
		UPDATE connector_targets
		SET config_json = '{"host":"127.0.0.1","port":1,"description":"closed port"}'
		WHERE id IN (?, ?)`,
		serverOne.TargetID,
		serverTwo.TargetID,
	); err != nil {
		t.Fatalf("move test ssh targets to closed port: %v", err)
	}

	missingConfirmation := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/bulk-exec", "", bulkConsoleCommandRequest{
		TargetIDs: []int64{serverOne.ID, serverTwo.ID},
		Command:   "hostname",
		Reason:    "bulk smoke",
	})
	if missingConfirmation.Code != http.StatusBadRequest || !strings.Contains(missingConfirmation.Body.String(), "RUN ON 2 TARGETS") {
		t.Fatalf("bulk command should require exact confirmation, got %d %s", missingConfirmation.Code, missingConfirmation.Body.String())
	}

	duplicateServer := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/bulk-exec", "", bulkConsoleCommandRequest{
		TargetIDs:    []int64{serverOne.ID, serverOne.ID},
		Command:      "hostname",
		Reason:       "bulk smoke",
		Confirmation: "RUN ON 2 TARGETS",
	})
	if duplicateServer.Code != http.StatusBadRequest || !strings.Contains(duplicateServer.Body.String(), "duplicates") {
		t.Fatalf("bulk command should reject duplicate targets, got %d %s", duplicateServer.Code, duplicateServer.Body.String())
	}

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/bulk-exec", "", bulkConsoleCommandRequest{
		TargetIDs:    []int64{serverOne.ID, serverTwo.ID},
		Command:      "hostname",
		Reason:       "bulk smoke",
		Confirmation: "RUN ON 2 TARGETS",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("bulk command failed: %d %s", response.Code, response.Body.String())
	}
	result := decodeRouteResponse[bulkConsoleCommandResponse](t, response.Body.Bytes())
	if result.Parallelism != bulkConsoleCommandParallelism || len(result.Items) != 2 {
		t.Fatalf("unexpected bulk command response: %#v", result)
	}
	waitForBulkCommandHistory(t, fixture.db, result.Items)

	var rows int
	if err := fixture.db.QueryRow(`
		SELECT COUNT(*)
		FROM command_requests
		WHERE source = 'manual'
			AND token_id IS NULL
			AND encrypted_command <> ''
			AND command = 'hostname'
			AND reason = 'bulk smoke'
			AND status IN ('running', 'error')`,
	).Scan(&rows); err != nil {
		t.Fatalf("count bulk command requests: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected two manual bulk command rows, got %d", rows)
	}

	var auditRows int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'console.bulk_exec.started'`).Scan(&auditRows); err != nil {
		t.Fatalf("count bulk command audit: %v", err)
	}
	if auditRows != 1 {
		t.Fatalf("expected one bulk audit event, got %d", auditRows)
	}
}

func TestCommandRequestInsertRollsBackWhenHistoryProjectionFails(t *testing.T) {
	fixture := newAPITestFixture(t)
	target := fixture.createKeyAndServer(t, "history-rollback")
	if _, err := fixture.db.Exec(`
		CREATE TRIGGER reject_command_history_insert
		BEFORE INSERT ON history_entries
		WHEN NEW.source_ref_type = 'command_request'
		BEGIN
			SELECT RAISE(ABORT, 'reject command history');
		END`); err != nil {
		t.Fatalf("install history rejection trigger: %v", err)
	}

	_, err := fixture.server.insertCommandRequestWithOptions(t.Context(), fixture.server.activeRuntime(), commandRequestInsert{
		RuntimeID: target.ID,
		Source:    commandRequestSourceManual,
		Command:   "echo rollback",
		Reason:    "atomic insert rollback",
		Status:    "running",
	})
	if err == nil || !strings.Contains(err.Error(), "reject command history") {
		t.Fatalf("expected history projection failure, got %v", err)
	}
	assertTableCount(t, fixture.db, "command_requests", 0)
	assertTableCount(t, fixture.db, "history_entries", 0)
	assertTableCount(t, fixture.db, "audit_outbox", 0)
}

func TestBulkConsoleCommandRollsBackEveryRequestWhenOneProjectionFails(t *testing.T) {
	fixture := newAPITestFixture(t)
	serverOne := fixture.createKeyAndServer(t, "bulk-rollback-one")
	serverTwo := fixture.createKeyAndServer(t, "bulk-rollback-two")
	if _, err := fixture.db.Exec(`
		CREATE TRIGGER reject_second_bulk_history_insert
		BEFORE INSERT ON history_entries
		WHEN NEW.source_ref_type = 'command_request' AND NEW.runtime_id = ` + strconv.FormatInt(serverTwo.ID, 10) + `
		BEGIN
			SELECT RAISE(ABORT, 'reject second bulk history');
		END`); err != nil {
		t.Fatalf("install bulk history rejection trigger: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/bulk-exec", "", bulkConsoleCommandRequest{
		TargetIDs:    []int64{serverOne.ID, serverTwo.ID},
		Command:      "echo atomic bulk",
		Reason:       "atomic bulk rollback",
		Confirmation: "RUN ON 2 TARGETS",
	})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected atomic bulk failure, got %d %s", response.Code, response.Body.String())
	}
	var requestCount int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM command_requests WHERE reason = 'atomic bulk rollback'`).Scan(&requestCount); err != nil {
		t.Fatalf("count rolled back bulk requests: %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected every bulk request to roll back, got %d", requestCount)
	}
	var auditCount int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE action = 'console.bulk_exec.started'`).Scan(&auditCount); err != nil {
		t.Fatalf("count rolled back bulk audit: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("expected bulk audit to roll back, got %d", auditCount)
	}
}

func waitForBulkCommandHistory(t *testing.T, database *sql.DB, items []bulkConsoleCommandResponseItem) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		completed := 0
		for _, item := range items {
			var status string
			err := database.QueryRow(`
				SELECT status
				FROM history_entries
				WHERE source_ref_type = 'command_request' AND source_ref_id = ?`,
				item.RequestID,
			).Scan(&status)
			if err == nil && status != "running" {
				completed++
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("read bulk command history: %v", err)
			}
		}
		if completed == len(items) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("bulk command history did not settle: completed=%d total=%d", completed, len(items))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHistoryAndAuditPaginationSearchAndDetail(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	now := time.Now().UTC().Format(time.RFC3339)
	dockerResult, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'docker ps', 'inspect docker containers', 'completed', 'docker output body', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert docker request: %v", err)
	}
	dockerID, err := dockerResult.LastInsertId()
	if err != nil {
		t.Fatalf("docker request id: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncCommandRequest(ctx, dockerID); err != nil {
		t.Fatalf("sync docker request to history: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'uptime', 'inspect uptime', 'completed', 'uptime output body', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert uptime request: %v", err)
	}
	historyResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?q=docker&limit=1", "", nil)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("history search failed: %d %s", historyResponse.Code, historyResponse.Body.String())
	}
	historyPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, historyResponse.Body.Bytes())
	if historyPage.Total != 1 || len(historyPage.Items) != 1 || historyPage.Items[0].SourceRefID != dockerID {
		t.Fatalf("unexpected history page: %#v", historyPage)
	}
	if historyPage.Items[0].OutputText != "" {
		t.Fatalf("history list should not include full output: %#v", historyPage.Items[0])
	}
	punctuationSearchResponse := performJSON(fixture.server.Handler(), http.MethodGet, `/api/history?q=docker%3A%28%22&limit=1`, "", nil)
	if punctuationSearchResponse.Code != http.StatusOK {
		t.Fatalf("history punctuation search should be sanitized: %d %s", punctuationSearchResponse.Code, punctuationSearchResponse.Body.String())
	}
	historyDetailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/command-requests/"+strconv.FormatInt(dockerID, 10), "", nil)
	if historyDetailResponse.Code != http.StatusOK || !strings.Contains(historyDetailResponse.Body.String(), "docker output body") {
		t.Fatalf("history detail should include output: %d %s", historyDetailResponse.Code, historyDetailResponse.Body.String())
	}
	unifiedDockerResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?q=docker&limit=1", "", nil)
	if unifiedDockerResponse.Code != http.StatusOK {
		t.Fatalf("unified history search failed: %d %s", unifiedDockerResponse.Code, unifiedDockerResponse.Body.String())
	}
	unifiedDockerPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, unifiedDockerResponse.Body.Bytes())
	if unifiedDockerPage.Total != 1 || len(unifiedDockerPage.Items) != 1 || unifiedDockerPage.Items[0].SourceRefID != dockerID {
		t.Fatalf("unexpected unified docker history page: %#v", unifiedDockerPage)
	}
	dockerHistoryID := unifiedDockerPage.Items[0].ID
	store := connectortargets.NewStore(fixture.db)
	sshConnectorRequest, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             server.TargetID,
		ProfileID:            server.ProfileID,
		ConnectorKind:        "ssh",
		ActionName:           "exec",
		Input:                map[string]any{"command": "whoami"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert ssh connector action request: %v", err)
	}
	if _, err := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:          sshConnectorRequest.ID,
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"stdout": "root\n"},
		DisplayText: "root\n",
	}); err != nil {
		t.Fatalf("finish ssh connector action request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(ctx, sshConnectorRequest.ID); err != nil {
		t.Fatalf("sync ssh connector action to history: %v", err)
	}
	sshTargetHistoryResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?connector_kind=ssh&target_id="+strconv.FormatInt(server.TargetID, 10)+"&limit=10", "", nil)
	if sshTargetHistoryResponse.Code != http.StatusOK {
		t.Fatalf("ssh target unified history filter failed: %d %s", sshTargetHistoryResponse.Code, sshTargetHistoryResponse.Body.String())
	}
	sshTargetHistoryPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, sshTargetHistoryResponse.Body.Bytes())
	if sshTargetHistoryPage.Total != 2 {
		t.Fatalf("ssh target filter should include live-console command and connector action rows, got %#v", sshTargetHistoryPage)
	}
	sshRuntimeHistoryResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?connector_kind=ssh&runtime_id="+strconv.FormatInt(server.ID, 10)+"&limit=10", "", nil)
	if sshRuntimeHistoryResponse.Code != http.StatusOK {
		t.Fatalf("ssh runtime unified history filter failed: %d %s", sshRuntimeHistoryResponse.Code, sshRuntimeHistoryResponse.Body.String())
	}
	sshRuntimeHistoryPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, sshRuntimeHistoryResponse.Body.Bytes())
	if sshRuntimeHistoryPage.Total != 1 || len(sshRuntimeHistoryPage.Items) != 1 || sshRuntimeHistoryPage.Items[0].SourceRefID != dockerID {
		t.Fatalf("ssh runtime filter should isolate the live-console command row, got %#v", sshRuntimeHistoryPage)
	}
	pgTarget, pgProfile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
	connectorRequest, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             pgTarget.ID,
		ProfileID:            pgProfile.ID,
		ConnectorKind:        "postgres",
		ActionName:           "query_readonly",
		Input:                map[string]any{"sql": "select customer from invoices where customer = 'needle_customer'"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert connector request: %v", err)
	}
	if _, err := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:     connectorRequest.ID,
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"rows": []any{map[string]any{"customer": "needle_customer"}},
		},
	}); err != nil {
		t.Fatalf("finish connector request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(ctx, connectorRequest.ID); err != nil {
		t.Fatalf("sync connector request to history: %v", err)
	}
	connectorJSONSearchResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?q=needle_customer&connector_kind=postgres&limit=1", "", nil)
	if connectorJSONSearchResponse.Code != http.StatusOK {
		t.Fatalf("unified connector json search failed: %d %s", connectorJSONSearchResponse.Code, connectorJSONSearchResponse.Body.String())
	}
	connectorJSONSearchPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, connectorJSONSearchResponse.Body.Bytes())
	if connectorJSONSearchPage.Total != 1 || len(connectorJSONSearchPage.Items) != 1 || connectorJSONSearchPage.Items[0].SourceRefID != connectorRequest.ID {
		t.Fatalf("unexpected unified connector json search page: %#v", connectorJSONSearchPage)
	}
	secondPGProfile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:            pgTarget.ID,
		ConnectorKind:       "postgres",
		Kind:                "username_password",
		Label:               "analytics",
		Public:              map[string]any{"username": "analytics_readonly"},
		EncryptedSecretJSON: "",
	})
	if err != nil {
		t.Fatalf("create second postgres profile: %v", err)
	}
	encryptedOtherSecret, err := recordcrypto.EncryptJSON(fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID, recordcrypto.ConnectorCredentialProfile, secondPGProfile.ID, map[string]any{"password": "other-secret"})
	if err != nil {
		t.Fatalf("encrypt second profile secret: %v", err)
	}
	if err := store.SetCredentialProfileEncryptedSecret(ctx, pgTarget.ID, secondPGProfile.ID, encryptedOtherSecret); err != nil {
		t.Fatalf("store second profile secret: %v", err)
	}
	secondConnectorRequest, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             pgTarget.ID,
		ProfileID:            secondPGProfile.ID,
		ConnectorKind:        "postgres",
		ActionName:           "get_tables",
		Input:                map[string]any{"schema": "public"},
		EncryptedPayloadJSON: "encrypted-payload-2",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert second connector request: %v", err)
	}
	if _, err := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:     secondConnectorRequest.ID,
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"tables": []any{"analytics_events"},
		},
	}); err != nil {
		t.Fatalf("finish second connector request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(ctx, secondConnectorRequest.ID); err != nil {
		t.Fatalf("sync second connector request to history: %v", err)
	}
	profileFilterResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?connector_kind=postgres&target_id="+strconv.FormatInt(pgTarget.ID, 10)+"&profile_id="+strconv.FormatInt(pgProfile.ID, 10)+"&limit=10", "", nil)
	if profileFilterResponse.Code != http.StatusOK {
		t.Fatalf("postgres profile history filter failed: %d %s", profileFilterResponse.Code, profileFilterResponse.Body.String())
	}
	profileFilterPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, profileFilterResponse.Body.Bytes())
	if profileFilterPage.Total != 1 || len(profileFilterPage.Items) != 1 || profileFilterPage.Items[0].SourceRefID != connectorRequest.ID {
		t.Fatalf("postgres profile filter should isolate one profile, got %#v", profileFilterPage)
	}
	labelResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/history-labels", "", createHistoryLabelRequest{Name: "issue-440"})
	if labelResponse.Code != http.StatusCreated {
		t.Fatalf("create history label failed: %d %s", labelResponse.Code, labelResponse.Body.String())
	}
	label := decodeRouteResponse[historyLabelRecord](t, labelResponse.Body.Bytes())
	reusedLabelResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/history-labels", "", createHistoryLabelRequest{Name: "issue-440"})
	if reusedLabelResponse.Code != http.StatusOK {
		t.Fatalf("reused history label should return ok, got %d %s", reusedLabelResponse.Code, reusedLabelResponse.Body.String())
	}
	attachResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/history/"+strconv.FormatInt(dockerHistoryID, 10)+"/labels", "", attachHistoryLabelRequest{Name: "docker"})
	if attachResponse.Code != http.StatusCreated || !strings.Contains(attachResponse.Body.String(), `"docker"`) {
		t.Fatalf("attach history label by name failed: %d %s", attachResponse.Code, attachResponse.Body.String())
	}
	attachExistingResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/history/"+strconv.FormatInt(dockerHistoryID, 10)+"/labels", "", attachHistoryLabelRequest{LabelID: label.ID})
	if attachExistingResponse.Code != http.StatusOK || !strings.Contains(attachExistingResponse.Body.String(), `"issue-440"`) {
		t.Fatalf("attach existing history label failed: %d %s", attachExistingResponse.Code, attachExistingResponse.Body.String())
	}
	labelListResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history-labels", "", nil)
	if labelListResponse.Code != http.StatusOK || !strings.Contains(labelListResponse.Body.String(), `"issue-440"`) || !strings.Contains(labelListResponse.Body.String(), `"docker"`) {
		t.Fatalf("list history labels failed: %d %s", labelListResponse.Code, labelListResponse.Body.String())
	}
	unifiedLabelResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?label_id="+strconv.FormatInt(label.ID, 10), "", nil)
	if unifiedLabelResponse.Code != http.StatusOK {
		t.Fatalf("unified history label filter failed: %d %s", unifiedLabelResponse.Code, unifiedLabelResponse.Body.String())
	}
	unifiedLabelPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, unifiedLabelResponse.Body.Bytes())
	if unifiedLabelPage.Total != 1 || len(unifiedLabelPage.Items) != 1 || unifiedLabelPage.Items[0].SourceRefID != dockerID || len(unifiedLabelPage.Items[0].Labels) != 2 {
		t.Fatalf("unexpected unified history label page: %#v", unifiedLabelPage)
	}
	detachResponse := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/history/"+strconv.FormatInt(dockerHistoryID, 10)+"/labels/"+strconv.FormatInt(label.ID, 10), "", nil)
	if detachResponse.Code != http.StatusOK || strings.Contains(detachResponse.Body.String(), `"issue-440"`) {
		t.Fatalf("detach history label failed: %d %s", detachResponse.Code, detachResponse.Body.String())
	}
	unifiedDetachedResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?label_id="+strconv.FormatInt(label.ID, 10), "", nil)
	unifiedDetachedPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, unifiedDetachedResponse.Body.Bytes())
	if unifiedDetachedResponse.Code != http.StatusOK || unifiedDetachedPage.Total != 0 {
		t.Fatalf("detached label should filter as empty in unified history: %d %#v", unifiedDetachedResponse.Code, unifiedDetachedPage)
	}
	missingDetachResponse := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/history/"+strconv.FormatInt(dockerHistoryID, 10)+"/labels/"+strconv.FormatInt(label.ID, 10), "", nil)
	if missingDetachResponse.Code != http.StatusNotFound {
		t.Fatalf("missing label relationship should return not found, got %d %s", missingDetachResponse.Code, missingDetachResponse.Body.String())
	}
	deleteLabelResponse := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/history-labels/"+strconv.FormatInt(label.ID, 10), "", nil)
	if deleteLabelResponse.Code != http.StatusOK {
		t.Fatalf("delete history label failed: %d %s", deleteLabelResponse.Code, deleteLabelResponse.Body.String())
	}
	filterDeletedLabelResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?label_id="+strconv.FormatInt(label.ID, 10), "", nil)
	filterDeletedLabelPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, filterDeletedLabelResponse.Body.Bytes())
	if filterDeletedLabelResponse.Code != http.StatusOK || filterDeletedLabelPage.Total != 0 {
		t.Fatalf("deleted label should filter as empty: %d %#v", filterDeletedLabelResponse.Code, filterDeletedLabelPage)
	}

	sensitivePayload := strings.Repeat("x", 700) + " docker image scan"
	fixture.server.writeObservationAudit(ctx, fixture.server.activeRuntime(), "user", &token.ID, server.ID, "docker.audit", map[string]any{
		"detail": sensitivePayload,
	})
	auditResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs?q=image&limit=1", "", nil)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("audit search failed: %d %s", auditResponse.Code, auditResponse.Body.String())
	}
	auditPage := decodeRouteResponse[pageResponse[auditLogRecord]](t, auditResponse.Body.Bytes())
	if auditPage.Total != 1 || len(auditPage.Items) != 1 || auditPage.Items[0].Action != "docker.audit" {
		t.Fatalf("unexpected audit page: %#v", auditPage)
	}
	if len(auditPage.Items[0].PayloadJSON) > 500 {
		t.Fatalf("audit list payload should be a preview, got %d bytes", len(auditPage.Items[0].PayloadJSON))
	}
	auditDetailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs/"+strconv.FormatInt(auditPage.Items[0].ID, 10), "", nil)
	if auditDetailResponse.Code != http.StatusOK || !strings.Contains(auditDetailResponse.Body.String(), "docker image scan") {
		t.Fatalf("audit detail should include full payload: %d %s", auditDetailResponse.Code, auditDetailResponse.Body.String())
	}
	fixture.server.writeObservationAudit(ctx, fixture.server.activeRuntime(), "user", &token.ID, server.ID, "runtime.audit", map[string]any{
		"detail": "runtime-only audit metadata",
	})
	runtimeAuditResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs?q=runtime-only", "", nil)
	if runtimeAuditResponse.Code != http.StatusOK {
		t.Fatalf("runtime audit search failed: %d %s", runtimeAuditResponse.Code, runtimeAuditResponse.Body.String())
	}
	runtimeAuditPage := decodeRouteResponse[pageResponse[auditLogRecord]](t, runtimeAuditResponse.Body.Bytes())
	if runtimeAuditPage.Total != 1 || len(runtimeAuditPage.Items) != 1 || runtimeAuditPage.Items[0].TargetName != "worker-1" {
		t.Fatalf("runtime audit target metadata missing: %#v", runtimeAuditPage)
	}
	fixture.server.writeObservationAudit(ctx, fixture.server.activeRuntime(), "mcp", &token.ID, 0, "connector_action.completed", map[string]any{
		"connector_kind":    "ssh",
		"target_id":         server.TargetID,
		"profile_id":        server.ProfileID,
		"action_request_id": int64(777),
		"detail":            "connector audit metadata",
	})
	connectorAuditResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs?q=connector+audit+metadata&connector_kind=ssh&target_id="+strconv.FormatInt(server.TargetID, 10), "", nil)
	if connectorAuditResponse.Code != http.StatusOK {
		t.Fatalf("connector audit filter failed: %d %s", connectorAuditResponse.Code, connectorAuditResponse.Body.String())
	}
	connectorAuditPage := decodeRouteResponse[pageResponse[auditLogRecord]](t, connectorAuditResponse.Body.Bytes())
	if connectorAuditPage.Total != 1 || len(connectorAuditPage.Items) != 1 {
		t.Fatalf("unexpected connector audit page: %#v", connectorAuditPage)
	}
	item := connectorAuditPage.Items[0]
	if item.ConnectorKind != "ssh" || item.TargetID == nil || *item.TargetID != server.TargetID || item.TargetName != "worker-1" || item.ActionRequestID == nil || *item.ActionRequestID != 777 {
		t.Fatalf("connector audit metadata missing: %#v", item)
	}
}
