package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestConnectorActionApprovalRoutesDeclinePendingRequest(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}

	listResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals?status=approval_pending", "", nil)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), strconv.FormatInt(result.Request.ID, 10)) {
		t.Fatalf("list connector approvals failed: %d %s", listResponse.Code, listResponse.Body.String())
	}
	detailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10), "", nil)
	if detailResponse.Code != http.StatusOK || !strings.Contains(detailResponse.Body.String(), "select 1") {
		t.Fatalf("approval detail must expose exact pending preview: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	var encryptedPayload string
	if err := fixture.db.QueryRow(`SELECT encrypted_payload_json FROM connector_action_requests WHERE id = ?`, result.Request.ID).Scan(&encryptedPayload); err != nil {
		t.Fatalf("read encrypted approval payload: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE connector_action_requests SET encrypted_payload_json = 'invalid' WHERE id = ?`, result.Request.ID); err != nil {
		t.Fatalf("corrupt encrypted approval payload: %v", err)
	}
	redactedListResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals?status=approval_pending", "", nil)
	if redactedListResponse.Code != http.StatusOK {
		t.Fatalf("approval list must not decrypt exact pending payloads: %d %s", redactedListResponse.Code, redactedListResponse.Body.String())
	}
	if _, err := fixture.db.Exec(`UPDATE connector_action_requests SET encrypted_payload_json = ? WHERE id = ?`, encryptedPayload, result.Request.ID); err != nil {
		t.Fatalf("restore encrypted approval payload: %v", err)
	}
	declineResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/decline", "", declineConnectorActionApprovalRequest{UserNote: "not now"})
	if declineResponse.Code != http.StatusOK || !strings.Contains(declineResponse.Body.String(), `"status":"declined"`) {
		t.Fatalf("decline connector approval failed: %d %s", declineResponse.Code, declineResponse.Body.String())
	}
	mcpResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-action-requests/"+strconv.FormatInt(result.Request.ID, 10), token.TokenValue, nil)
	if mcpResponse.Code != http.StatusOK || !strings.Contains(mcpResponse.Body.String(), `"status":"declined"`) || !strings.Contains(mcpResponse.Body.String(), "not now") {
		t.Fatalf("mcp connector request should show decline: %d %s", mcpResponse.Code, mcpResponse.Body.String())
	}
}

func TestConnectorActionApprovalRunUsesEncryptedInputNotRedactedDisplay(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 'password=super-secret' as value"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if !strings.Contains(fmt.Sprint(result.Request.Input["sql"]), "password=[REDACTED]") {
		t.Fatalf("display input should be redacted: %#v", result.Request.Input)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{})
	if runResponse.Code != http.StatusOK {
		t.Fatalf("approval run should not fail stale because display input was redacted: %d %s", runResponse.Code, runResponse.Body.String())
	}
	finished, err := store.GetActionRequest(context.Background(), result.Request.ID)
	if err != nil {
		t.Fatalf("get finished connector request: %v", err)
	}
	if finished.Status == connectors.ResultStale {
		t.Fatalf("request should not become stale from redacted display input: %#v", finished)
	}
}

func TestConnectorActionApprovalRunDeliversUserNote(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{UserNote: "only inspect metadata"})
	if runResponse.Code != http.StatusOK {
		t.Fatalf("approval run failed: %d %s", runResponse.Code, runResponse.Body.String())
	}
	var queued string
	if err := fixture.db.QueryRow(`
		SELECT message
		FROM message_queue
		WHERE token_id = ? AND direction = 'user_to_ai'
		ORDER BY id DESC
		LIMIT 1`,
		token.ID,
	).Scan(&queued); err != nil {
		t.Fatalf("read queued approval note: %v", err)
	}
	if !strings.Contains(queued, "only inspect metadata") {
		t.Fatalf("queued note = %q", queued)
	}
}

func TestConnectorActionApprovalRunMarksDriftStale(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionBlocked,
	}); err != nil {
		t.Fatalf("block connector permission: %v", err)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{})
	if runResponse.Code != http.StatusConflict || !strings.Contains(runResponse.Body.String(), "fresh request") {
		t.Fatalf("expected stale conflict, got %d %s", runResponse.Code, runResponse.Body.String())
	}
	stale, err := store.GetActionRequest(context.Background(), result.Request.ID)
	if err != nil {
		t.Fatalf("get stale connector request: %v", err)
	}
	if stale.Status != connectors.ResultStale {
		t.Fatalf("status = %q", stale.Status)
	}
	if stale.ApprovalContextDrift != "permission" {
		t.Fatalf("approval drift = %q", stale.ApprovalContextDrift)
	}
}

func TestConnectorActionApprovalRunMarksPrepareFailureStale(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	badAction := "missing_action"
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    badAction,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	encryptedPayload, err := fixture.server.activeRuntime().vault.EncryptJSON(connectorActionExecutionEnvelope{
		Input:   map[string]any{},
		Payload: map[string]any{},
	})
	if err != nil {
		t.Fatalf("encrypt pending payload: %v", err)
	}
	request, err := store.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           badAction,
		Source:               commandRequestSourceMCP,
		Input:                map[string]any{},
		EncryptedPayloadJSON: encryptedPayload,
		Status:               connectors.ResultApprovalPending,
		ApprovalContextHash:  "old-context",
	})
	if err != nil {
		t.Fatalf("insert pending connector request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(context.Background(), request.ID); err != nil {
		t.Fatalf("sync pending connector request: %v", err)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{})
	if runResponse.Code != http.StatusConflict || !strings.Contains(runResponse.Body.String(), "fresh request") {
		t.Fatalf("expected prepare drift conflict, got %d %s", runResponse.Code, runResponse.Body.String())
	}
	stale, err := store.GetActionRequest(context.Background(), request.ID)
	if err != nil {
		t.Fatalf("get stale connector request: %v", err)
	}
	if stale.Status != connectors.ResultStale || !strings.Contains(stale.Error, "fresh request") {
		t.Fatalf("request should be stale with fresh-request error: %#v", stale)
	}
	if stale.ApprovalContextDrift != "target_or_action" {
		t.Fatalf("approval drift = %q", stale.ApprovalContextDrift)
	}
	var historyStatus string
	if err := fixture.db.QueryRow(`
		SELECT status
		FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`,
		request.ID,
	).Scan(&historyStatus); err != nil {
		t.Fatalf("read synced history: %v", err)
	}
	if historyStatus != string(connectors.ResultStale) {
		t.Fatalf("history status = %q", historyStatus)
	}
}

func TestConnectorTargetAndProfileUpdatesStalePendingApprovals(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	pendingTarget, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           postgresconnector.ActionQueryReadonly,
		Input:                map[string]any{"sql": "select 1"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultApprovalPending,
	})
	if err != nil {
		t.Fatalf("insert pending target request: %v", err)
	}
	running, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           postgresconnector.ActionQueryReadonly,
		Input:                map[string]any{"sql": "select pg_sleep(10)"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert running request: %v", err)
	}
	updateTarget := performJSON(fixture.server.Handler(), http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", updateConnectorTargetRequest{
		Name: "main-db-renamed",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5432,
			"database":        "app",
			"ssl_mode":        "prefer",
		},
	})
	if updateTarget.Code != http.StatusOK {
		t.Fatalf("update connector target failed: %d %s", updateTarget.Code, updateTarget.Body.String())
	}
	gotPendingTarget, err := store.GetActionRequest(ctx, pendingTarget.ID)
	if err != nil {
		t.Fatalf("get pending target request: %v", err)
	}
	if gotPendingTarget.Status != connectors.ResultStale || gotPendingTarget.ApprovalContextDrift != "target" {
		t.Fatalf("pending target request should be stale after target update: %#v", gotPendingTarget)
	}
	gotRunning, err := store.GetActionRequest(ctx, running.ID)
	if err != nil {
		t.Fatalf("get running request: %v", err)
	}
	if gotRunning.Status != connectors.ResultRunning {
		t.Fatalf("running request should not be stale from target update: %#v", gotRunning)
	}

	pendingProfile, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           postgresconnector.ActionGetSchemas,
		Input:                map[string]any{},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultApprovalPending,
	})
	if err != nil {
		t.Fatalf("insert pending profile request: %v", err)
	}
	updateProfile := performJSON(fixture.server.Handler(), http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles/"+strconv.FormatInt(profile.ID, 10), "", updateConnectorCredentialProfileRequest{
		Kind:  "username_password",
		Label: "readonly-renamed",
		Public: map[string]any{
			"username": "app_readonly",
		},
		RiskLabel: "read-only",
	})
	if updateProfile.Code != http.StatusOK {
		t.Fatalf("update connector profile failed: %d %s", updateProfile.Code, updateProfile.Body.String())
	}
	gotPendingProfile, err := store.GetActionRequest(ctx, pendingProfile.ID)
	if err != nil {
		t.Fatalf("get pending profile request: %v", err)
	}
	if gotPendingProfile.Status != connectors.ResultStale || gotPendingProfile.ApprovalContextDrift != "profile" {
		t.Fatalf("pending profile request should be stale after profile update: %#v", gotPendingProfile)
	}
}

func TestConnectorActionApprovalRunRequiresCurrentToken(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, apiTestFixture, tokens.CreateResponse){
		"revoked": func(t *testing.T, fixture apiTestFixture, token tokens.CreateResponse) {
			t.Helper()
			if _, err := fixture.tokens.Revoke(context.Background(), token.ID); err != nil {
				t.Fatalf("revoke token: %v", err)
			}
		},
		"expired": func(t *testing.T, fixture apiTestFixture, token tokens.CreateResponse) {
			t.Helper()
			if _, err := fixture.db.Exec(`UPDATE api_tokens SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), token.ID); err != nil {
				t.Fatalf("expire token: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newAPITestFixture(t)
			store := connectortargets.NewStore(fixture.db)
			token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
			if err != nil {
				t.Fatalf("create token: %v", err)
			}
			target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
			if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
				TokenID:       token.ID,
				TargetID:      target.ID,
				ProfileID:     profile.ID,
				ActionName:    postgresconnector.ActionQueryReadonly,
				ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
			}); err != nil {
				t.Fatalf("set connector permission: %v", err)
			}
			result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
				Source:     commandRequestSourceMCP,
				TokenID:    token.ID,
				TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
				ActionName: postgresconnector.ActionQueryReadonly,
				Input:      map[string]any{"sql": "select 1"},
				Reason:     "smoke",
			})
			if err != nil {
				t.Fatalf("call connector action: %v", err)
			}

			mutate(t, fixture, token)
			runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{})
			if runResponse.Code != http.StatusConflict || !strings.Contains(runResponse.Body.String(), "fresh request") {
				t.Fatalf("expected stale conflict, got %d %s", runResponse.Code, runResponse.Body.String())
			}
			stale, err := store.GetActionRequest(context.Background(), result.Request.ID)
			if err != nil {
				t.Fatalf("get stale connector request: %v", err)
			}
			if stale.Status != connectors.ResultStale {
				t.Fatalf("status = %q", stale.Status)
			}
		})
	}
}
