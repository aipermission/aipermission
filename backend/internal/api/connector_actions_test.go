package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestRuntimePrepareConnectorActionUsesSSHConnectorProfile(t *testing.T) {
	database := openAPITestDB(t)
	profile := createTestSSHConnectorProfile(t, database, sshkeys.NewStore(database, openAPITestVault(t)), "core-1")
	targetRef := profile.TargetRef
	runtime := &databaseRuntime{database: database}

	prepared, err := runtime.prepareConnectorAction(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  targetRef,
		ActionName: sshconnector.ActionExec,
		Input:      map[string]any{"command": "uptime"},
		Reason:     "smoke",
		CreatedAt:  time.Date(2026, 6, 9, 12, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("prepare connector action: %v", err)
	}

	if prepared.Action.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", prepared.Action.ConnectorKind)
	}
	if prepared.Action.TargetRef != targetRef {
		t.Fatalf("target ref = %q", prepared.Action.TargetRef)
	}
	if prepared.Action.ProfileID < 1 {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Payload["command"] != "uptime" {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func TestConnectorRuntimeServicesAreKindScoped(t *testing.T) {
	if services := connectorRuntimeServices(postgresconnector.Kind, &Server{}, &databaseRuntime{}); len(services) != 0 {
		t.Fatalf("postgres should not receive ssh runtime services: %#v", services)
	}
	services := connectorRuntimeServices(sshconnector.Kind, &Server{}, &databaseRuntime{})
	if services == nil || services[sshconnector.RuntimeServiceName] == nil {
		t.Fatalf("ssh runtime service missing: %#v", services)
	}
}

func TestConnectorApprovalContextHashesConnectorAndActionDefinition(t *testing.T) {
	prepared := actions.PreparedRequest{
		Target: connectors.TargetView{
			ID:            1,
			Ref:           "postgres:1:2",
			ConnectorKind: postgresconnector.Kind,
			Name:          "main-db",
			Config:        map[string]any{"host": "127.0.0.1", "database": "app"},
		},
		Profile: connectors.CredentialProfileView{
			ID:             2,
			TargetID:       1,
			Kind:           "username_password",
			Label:          "readonly",
			Public:         map[string]any{"username": "app_readonly"},
			RiskLabel:      "read-only",
			UpdatedAt:      "2026-06-12T11:59:00Z",
			SecretRevision: "secret-revision-a",
		},
		ConnectorVersion: "0.1",
		ActionDefinition: connectors.ActionDefinition{
			Name:        postgresconnector.ActionQueryReadonly,
			Label:       "Query read-only",
			Description: "Run bounded read-only SQL.",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "sql", Type: connectors.FieldString, Required: true},
			}},
		},
		Action: connectors.PreparedAction{
			ConnectorKind: postgresconnector.Kind,
			TargetRef:     "postgres:1:2",
			ActionName:    postgresconnector.ActionQueryReadonly,
			Risk:          connectors.RiskRead,
			Payload:       map[string]any{"sql": "select 1"},
		},
		Requested: actions.PrepareRequest{
			Source:     commandRequestSourceMCP,
			TargetRef:  "postgres:1:2",
			ActionName: postgresconnector.ActionQueryReadonly,
			Input:      map[string]any{"sql": "select 1"},
		},
	}
	token := tokens.Token{ID: 3, Name: "codex"}
	permission := connectortargets.ActionPermission{
		TokenID:       token.ID,
		TargetID:      prepared.Target.ID,
		ProfileID:     prepared.Profile.ID,
		ActionName:    prepared.Action.ActionName,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}

	_, baseHash, err := connectorApprovalContext(prepared, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context: %v", err)
	}
	versionChanged := prepared
	versionChanged.ConnectorVersion = "0.2"
	_, versionHash, err := connectorApprovalContext(versionChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with version change: %v", err)
	}
	if versionHash == baseHash {
		t.Fatalf("connector version drift should change approval hash")
	}
	actionChanged := prepared
	actionChanged.ActionDefinition.Description = "Run read-only SQL with a different contract."
	_, actionHash, err := connectorApprovalContext(actionChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with action definition change: %v", err)
	}
	if actionHash == baseHash {
		t.Fatalf("action definition drift should change approval hash")
	}
	profileChanged := prepared
	profileChanged.Profile.SecretRevision = "secret-revision-b"
	_, profileHash, err := connectorApprovalContext(profileChanged, token, permission, "2026-06-12T12:00:00Z")
	if err != nil {
		t.Fatalf("approval context with profile revision change: %v", err)
	}
	if profileHash == baseHash {
		t.Fatalf("credential profile revision drift should change approval hash")
	}
}

func TestCallConnectorActionBlocksMissingPermission(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)

	result, err := server.callConnectorAction(context.Background(), runtime, connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    tokenID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if result.Result.Status != connectors.ResultBlocked || result.Request.Status != connectors.ResultBlocked {
		t.Fatalf("expected blocked result/request, got %#v", result)
	}
	if result.Request.CompletedAt == nil || result.Request.Error == "" {
		t.Fatalf("blocked request should be terminal with error: %#v", result.Request)
	}
}

func TestCallConnectorActionCreatesPendingApproval(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set action permission: %v", err)
	}

	result, err := server.callConnectorAction(context.Background(), runtime, connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    tokenID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1", "max_rows": 5},
		Reason:     "inspect one row",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if result.Result.Status != connectors.ResultApprovalPending || result.Request.Status != connectors.ResultApprovalPending {
		t.Fatalf("expected pending result/request, got %#v", result)
	}
	if result.Request.EncryptedPayloadJSON == "" || result.Request.ApprovalContextHash == "" {
		t.Fatalf("pending request missing encrypted payload/context: %#v", result.Request)
	}
	if result.Result.Handles.RequestID != result.Request.ID || result.Result.Handles.FollowupTool == "" {
		t.Fatalf("pending result missing followup handle: %#v", result.Result)
	}
}

func TestInsertConnectorActionRequestRedactsDisplayedInputOnly(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	targetView, profileView, err := store.ResolveConnectorActionTarget(context.Background(), connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID))
	if err != nil {
		t.Fatalf("resolve target/profile: %v", err)
	}
	rawInput := map[string]any{
		"sql":          "select 'password=super-secret' as value",
		"access_token": "raw-access-token",
		"nested":       map[string]any{"authorization": "Bearer raw-bearer-token"},
	}
	prepared := actions.PreparedRequest{
		Target:  targetView,
		Profile: profileView,
		ActionDefinition: connectors.ActionDefinition{
			Name: postgresconnector.ActionQueryReadonly,
			OutputHint: connectors.OutputHint{
				SensitiveFields: []string{"access_token"},
			},
		},
		Action: connectors.PreparedAction{
			ConnectorKind: postgresconnector.Kind,
			TargetRef:     targetView.Ref,
			ProfileID:     profile.ID,
			ActionName:    postgresconnector.ActionQueryReadonly,
			Payload:       rawInput,
		},
		Requested: actions.PrepareRequest{
			Source:     commandRequestSourceMCP,
			TargetRef:  targetView.Ref,
			ActionName: postgresconnector.ActionQueryReadonly,
			Input:      rawInput,
			Reason:     "Bearer raw-reason-token password=reason-secret",
		},
	}

	request, err := server.insertConnectorActionRequest(context.Background(), runtime, tokenID, prepared, connectortargets.ActionPermission{}, connectors.ResultApprovalPending, "")
	if err != nil {
		t.Fatalf("insert connector action request: %v", err)
	}
	var inputJSON string
	var reason string
	var encryptedPayload string
	if err := database.QueryRow(`
		SELECT input_json, reason, encrypted_payload_json
		FROM connector_action_requests
		WHERE id = ?`,
		request.ID,
	).Scan(&inputJSON, &reason, &encryptedPayload); err != nil {
		t.Fatalf("read connector action request: %v", err)
	}
	for _, secret := range []string{"super-secret", "raw-access-token", "raw-bearer-token", "raw-reason-token", "reason-secret"} {
		if strings.Contains(inputJSON, secret) || strings.Contains(reason, secret) {
			t.Fatalf("persisted connector request leaked %q: input=%s reason=%s", secret, inputJSON, reason)
		}
	}
	if !strings.Contains(inputJSON, `"access_token":"[REDACTED]"`) || !strings.Contains(inputJSON, `"authorization":"[REDACTED]"`) || !strings.Contains(inputJSON, `password=[REDACTED]`) {
		t.Fatalf("input was not redacted as expected: %s", inputJSON)
	}
	var historyInputJSON string
	if err := database.QueryRow(`
		SELECT input_json
		FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`,
		request.ID,
	).Scan(&historyInputJSON); err != nil {
		t.Fatalf("read connector history input: %v", err)
	}
	if historyInputJSON != inputJSON {
		t.Fatalf("history input drifted from redacted request input: history=%s request=%s", historyInputJSON, inputJSON)
	}
	mcpResponse := connectorActionRequestToMCPResponse(request)
	approvalResponse := connectorActionApprovalItemFromRequest(request)
	if mcpResponse.Input["access_token"] != "[REDACTED]" || approvalResponse.Input["access_token"] != "[REDACTED]" {
		t.Fatalf("response input was not redacted: mcp=%#v approval=%#v", mcpResponse.Input, approvalResponse.Input)
	}
	var decryptedPayload map[string]any
	if err := secretVault.DecryptJSON(encryptedPayload, &decryptedPayload); err != nil {
		t.Fatalf("decrypt execution payload: %v", err)
	}
	if decryptedPayload["access_token"] != "raw-access-token" || !strings.Contains(decryptedPayload["sql"].(string), "super-secret") {
		t.Fatalf("encrypted execution payload should preserve raw input: %#v", decryptedPayload)
	}
}

func TestRunningConnectorActionResponseRedactsOutput(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	request, err := store.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: postgresconnector.Kind,
		ActionName:    postgresconnector.ActionQueryReadonly,
		Input:         map[string]any{"sql": "select 1"},
		Status:        connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert running request: %v", err)
	}
	redacted := server.redactConnectorActionResult(context.Background(), runtime, connectors.ActionResult{
		Status:      connectors.ResultRunning,
		Output:      map[string]any{"rows": []any{map[string]any{"session_token": "raw-token", "name": "safe"}}},
		DisplayText: "Bearer raw-bearer-token",
		Error:       "password=super-secret",
	}, connectors.OutputHint{SensitiveFields: []string{"session_token"}})
	response := connectorActionToMCPResponse(request, redacted)
	payload := fmt.Sprint(response.Output, response.DisplayText, response.Error)
	for _, secret := range []string{"raw-token", "raw-bearer-token", "super-secret"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("running response leaked %q: %#v", secret, response)
		}
	}
}

func TestFinishConnectorActionRequestRedactsErrorAndHistory(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)

	request, err := store.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:              &tokenID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           postgresconnector.ActionQueryReadonly,
		Input:                map[string]any{"sql": "select 1"},
		EncryptedPayloadJSON: "encrypted",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert action request: %v", err)
	}
	finished, err := server.finishConnectorActionRequest(
		context.Background(),
		runtime,
		request.ID,
		connectors.ResultFailed,
		map[string]any{
			"rows": []any{
				map[string]any{
					"customer_secret": "visible-only-if-buggy",
					"token":           "token-value",
					"access_token":    "access-token-value",
					"name":            "safe",
				},
			},
		},
		"",
		"connect failed password=super-secret Bearer abcdefghijklmnopqrstuvwxyz",
		connectors.OutputHint{SensitiveFields: []string{"customer_secret"}},
	)
	if err != nil {
		t.Fatalf("finish action request: %v", err)
	}
	if strings.Contains(finished.Error, "super-secret") || strings.Contains(finished.Error, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("finished error leaked secret: %q", finished.Error)
	}
	if !strings.Contains(finished.Error, "password=[REDACTED]") || !strings.Contains(finished.Error, "Bearer [REDACTED]") {
		t.Fatalf("finished error was not redacted as expected: %q", finished.Error)
	}
	var historyError string
	if err := database.QueryRow(`
		SELECT error
		FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`,
		request.ID,
	).Scan(&historyError); err != nil {
		t.Fatalf("read history error: %v", err)
	}
	if historyError != finished.Error {
		t.Fatalf("history error drifted from finished request: history=%q finished=%q", historyError, finished.Error)
	}
	response := connectorActionToMCPResponse(finished, connectors.ActionResult{Status: connectors.ResultFailed, Error: finished.Error})
	if strings.Contains(response.Error, "super-secret") || strings.Contains(response.Error, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("mcp response leaked secret: %q", response.Error)
	}
	if response.Error != finished.Error {
		t.Fatalf("mcp response error drifted from finished request: response=%q finished=%q", response.Error, finished.Error)
	}
	var outputJSON string
	if err := database.QueryRow(`
		SELECT output_json
		FROM connector_action_requests
		WHERE id = ?`,
		request.ID,
	).Scan(&outputJSON); err != nil {
		t.Fatalf("read connector action output: %v", err)
	}
	if strings.Contains(outputJSON, "visible-only-if-buggy") || strings.Contains(outputJSON, "token-value") || strings.Contains(outputJSON, "access-token-value") {
		t.Fatalf("structured connector output leaked sensitive field values: %s", outputJSON)
	}
	if !strings.Contains(outputJSON, `"customer_secret":"[REDACTED]"`) || !strings.Contains(outputJSON, `"token":"[REDACTED]"`) || !strings.Contains(outputJSON, `"access_token":"[REDACTED]"`) || !strings.Contains(outputJSON, `"name":"safe"`) {
		t.Fatalf("structured connector output was not field-redacted as expected: %s", outputJSON)
	}
}

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
	declineResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/decline", "", declineApprovalRequest{UserNote: "not now"})
	if declineResponse.Code != http.StatusOK || !strings.Contains(declineResponse.Body.String(), `"status":"declined"`) {
		t.Fatalf("decline connector approval failed: %d %s", declineResponse.Code, declineResponse.Body.String())
	}
	mcpResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-action-requests/"+strconv.FormatInt(result.Request.ID, 10), token.TokenValue, nil)
	if mcpResponse.Code != http.StatusOK || !strings.Contains(mcpResponse.Body.String(), `"status":"declined"`) || !strings.Contains(mcpResponse.Body.String(), "not now") {
		t.Fatalf("mcp connector request should show decline: %d %s", mcpResponse.Code, mcpResponse.Body.String())
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

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runApprovalRequest{})
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
	request, err := store.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           badAction,
		Source:               commandRequestSourceMCP,
		Input:                map[string]any{},
		EncryptedPayloadJSON: "{}",
		Status:               connectors.ResultApprovalPending,
		ApprovalContextHash:  "old-context",
	})
	if err != nil {
		t.Fatalf("insert pending connector request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(context.Background(), request.ID); err != nil {
		t.Fatalf("sync pending connector request: %v", err)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(request.ID, 10)+"/run", "", runApprovalRequest{})
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
			runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runApprovalRequest{})
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

func openAPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func connectorActionTestRuntime(database *sql.DB, secretVault *vault.Vault) *databaseRuntime {
	return &databaseRuntime{
		database: database,
		vault:    secretVault,
		tokens:   tokens.NewStore(database),
	}
}

func openAPITestVault(t *testing.T) *vault.Vault {
	t.Helper()
	secretVault, err := vault.New("test-gateway-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	return secretVault
}

func createAPITestPostgresTargetProfile(t *testing.T, store *connectortargets.Store, secretVault *vault.Vault) (connectortargets.Target, connectortargets.CredentialProfile) {
	t.Helper()
	ctx := context.Background()
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: postgresconnector.Kind,
		Name:          "main-db",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5432,
			"database":        "app",
			"ssl_mode":        "disable",
		},
	})
	if err != nil {
		t.Fatalf("create postgres target: %v", err)
	}
	encryptedSecret, err := secretVault.EncryptJSON(map[string]any{"password": "secret"})
	if err != nil {
		t.Fatalf("encrypt profile secret: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       postgresconnector.Kind,
		Kind:                "username_password",
		Label:               "readonly",
		Public:              map[string]any{"username": "app_readonly"},
		EncryptedSecretJSON: encryptedSecret,
	})
	if err != nil {
		t.Fatalf("create postgres profile: %v", err)
	}
	return target, profile
}

func insertAPITestToken(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('connector-codex', 'connector-hash', 'aip_conn', ?, ?)`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("token id: %v", err)
	}
	return id
}
