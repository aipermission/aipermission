package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestCallConnectorActionBlocksMissingPermission(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
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
	runtime := connectorActionTestRuntime(t, database, secretVault)
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

func TestRunLocalConnectorActionCreatesManualHistory(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	registry := connectors.NewRegistry()
	if err := registry.Register(localActionTestConnector{}); err != nil {
		t.Fatalf("register local test connector: %v", err)
	}
	runtime := &databaseRuntime{
		database: database,
		vault:    secretVault,
		tokens:   tokens.NewStore(database),
		registry: registry,
	}
	server := &Server{}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(context.Background(), connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind,
		Name:          "local-target",
		Config:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create local target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(context.Background(), connectortargets.CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: localActionTestConnectorKind,
		Kind:          "default",
		Label:         "main",
		Public:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create local profile: %v", err)
	}

	result, err := server.runLocalConnectorAction(context.Background(), runtime, connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "hello"},
		Reason:     "manual console smoke",
	})
	if err != nil {
		t.Fatalf("run local connector action: %v", err)
	}
	if result.Request.TokenID != nil || result.Request.Source != commandRequestSourceManual || result.Request.Status != connectors.ResultCompleted {
		t.Fatalf("unexpected local request: %#v", result.Request)
	}
	if result.Result.Output.(map[string]any)["echo"] != "hello" {
		t.Fatalf("unexpected output: %#v", result.Result.Output)
	}
	var historyCount int
	if err := database.QueryRow(`
		SELECT COUNT(*)
		FROM history_entries
		WHERE source_ref_type = ? AND source_ref_id = ? AND source = 'manual' AND connector_kind = ?`,
		historypkg.SourceConnectorActionRequest,
		result.Request.ID,
		localActionTestConnectorKind,
	).Scan(&historyCount); err != nil {
		t.Fatalf("read local action history: %v", err)
	}
	if historyCount != 1 {
		t.Fatalf("expected one local action history row, got %d", historyCount)
	}

	completedWithHandle, err := server.runLocalConnectorAction(context.Background(), runtime, connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "with-handle"},
		Reason:     "capture completed session handle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if completedWithHandle.Request.Status != connectors.ResultCompleted ||
		completedWithHandle.Request.SessionID == nil || *completedWithHandle.Request.SessionID != 123 ||
		completedWithHandle.Request.SessionGeneration == nil || *completedWithHandle.Request.SessionGeneration != 456 {
		t.Fatalf("completed action handle was not persisted: %#v", completedWithHandle.Request)
	}

	incompleteHandle, err := server.runLocalConnectorAction(context.Background(), runtime, connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "incomplete-handle"},
		Reason:     "reject incomplete session handle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if incompleteHandle.Request.Status != connectors.ResultFailed ||
		incompleteHandle.Result.Status != connectors.ResultFailed ||
		!strings.Contains(incompleteHandle.Result.Error, "incomplete session handle") {
		t.Fatalf("incomplete session handle should fail terminally: %#v", incompleteHandle)
	}

	classifiedFailure, err := server.runLocalConnectorAction(context.Background(), runtime, connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "classified-error"},
		Reason:     "preserve a stable connector error code",
	})
	if err != nil {
		t.Fatal(err)
	}
	output, _ := classifiedFailure.Request.Output.(map[string]any)
	if classifiedFailure.Request.Status != connectors.ResultFailed || output["code"] != "fixture_failure" || classifiedFailure.Result.Output.(map[string]any)["code"] != "fixture_failure" {
		t.Fatalf("classified connector error code was not persisted: %#v", classifiedFailure)
	}
}

func TestInsertConnectorActionRequestRedactsDisplayedInputOnly(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	if _, err := insertRedactionRule(t.Context(), runtime, redactionRuleRequest{
		Name: "approval preview token", Pattern: `internal_[a-z0-9]+`, Enabled: true,
	}); err != nil {
		t.Fatalf("insert custom redaction rule: %v", err)
	}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	targetView, profileView, err := store.ResolveConnectorActionTarget(context.Background(), connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID))
	if err != nil {
		t.Fatalf("resolve target/profile: %v", err)
	}
	rawInput := map[string]any{
		"sql":            "select 'password=super-secret' as value",
		"access_token":   "raw-access-token",
		"opaque_message": "arbitrary-publish-content",
		"nested":         map[string]any{"authorization": "Bearer raw-bearer-token"},
	}
	prepared := actions.PreparedRequest{
		Target:  targetView,
		Profile: profileView,
		ActionDefinition: connectors.ActionDefinition{
			Name:                 postgresconnector.ActionQueryReadonly,
			SensitiveInputFields: []string{"access_token", "opaque_message", "client-secret"},
		},
		Action: connectors.PreparedAction{
			ConnectorKind: postgresconnector.Kind,
			TargetRef:     targetView.Ref,
			ProfileID:     profile.ID,
			ActionName:    postgresconnector.ActionQueryReadonly,
			Preview:       map[string]any{"body": "password=visible-for-approval internal_abc123", "opaque_message": "exact-sensitive-preview", "client_secret": "hyphen-normalized-preview-secret"},
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
	var previewJSON string
	var reason string
	var encryptedPayload string
	if err := database.QueryRow(`
			SELECT input_json, preview_json, reason, encrypted_payload_json
		FROM connector_action_requests
		WHERE id = ?`,
		request.ID,
	).Scan(&inputJSON, &previewJSON, &reason, &encryptedPayload); err != nil {
		t.Fatalf("read connector action request: %v", err)
	}
	for _, secret := range []string{"super-secret", "raw-access-token", "arbitrary-publish-content", "raw-bearer-token", "raw-reason-token", "reason-secret"} {
		if strings.Contains(inputJSON, secret) || strings.Contains(reason, secret) {
			t.Fatalf("persisted connector request leaked %q: input=%s reason=%s", secret, inputJSON, reason)
		}
	}
	if !strings.Contains(inputJSON, `"access_token":"[REDACTED]"`) || !strings.Contains(inputJSON, `"opaque_message":"[REDACTED]"`) || !strings.Contains(inputJSON, `"authorization":"[REDACTED]"`) || !strings.Contains(inputJSON, `password=[REDACTED]`) {
		t.Fatalf("input was not redacted as expected: %s", inputJSON)
	}
	if strings.Contains(previewJSON, "visible-for-approval") || strings.Contains(previewJSON, "internal_abc123") {
		t.Fatalf("persisted display preview leaked exact approval content: %s", previewJSON)
	}
	if strings.Contains(previewJSON, "exact-sensitive-preview") || !strings.Contains(previewJSON, `"opaque_message":"[REDACTED]"`) {
		t.Fatalf("persisted display preview did not apply sensitive input fields: %s", previewJSON)
	}
	if strings.Contains(previewJSON, "hyphen-normalized-preview-secret") || !strings.Contains(previewJSON, `"client_secret":"[REDACTED]"`) {
		t.Fatalf("persisted display preview did not normalize declared sensitive fields: %s", previewJSON)
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
	if mcpResponse.Input["opaque_message"] != "[REDACTED]" || approvalResponse.Input["opaque_message"] != "[REDACTED]" {
		t.Fatalf("action-declared sensitive input was not redacted: mcp=%#v approval=%#v", mcpResponse.Input, approvalResponse.Input)
	}
	exactApproval, err := connectorActionApprovalItemForResponse(runtime, request)
	if err != nil {
		t.Fatalf("build exact approval response: %v", err)
	}
	if exactApproval.Preview["body"] != "password=visible-for-approval internal_abc123" {
		t.Fatalf("pending approval preview must match the exact prepared action: %#v", exactApproval.Preview)
	}
	if exactApproval.Preview["opaque_message"] != "exact-sensitive-preview" {
		t.Fatalf("exact approval preview must preserve sensitive content inside the encrypted envelope: %#v", exactApproval.Preview)
	}
	if exactApproval.Preview["client_secret"] != "hyphen-normalized-preview-secret" {
		t.Fatalf("exact approval preview must preserve normalized sensitive content inside the encrypted envelope: %#v", exactApproval.Preview)
	}
	redactedApproval := connectorActionApprovalItemFromRequest(request)
	if redactedApproval.Preview["body"] == "password=visible-for-approval internal_abc123" {
		t.Fatalf("approval list projection exposed exact preview: %#v", redactedApproval.Preview)
	}
	var decryptedPayload connectorActionExecutionEnvelope
	if err := secretVault.DecryptJSON(encryptedPayload, &decryptedPayload); err != nil {
		t.Fatalf("decrypt execution payload: %v", err)
	}
	if decryptedPayload.Input["access_token"] != "raw-access-token" || !strings.Contains(decryptedPayload.Input["sql"].(string), "super-secret") {
		t.Fatalf("encrypted execution payload should preserve raw input: %#v", decryptedPayload)
	}
	if decryptedPayload.Input["opaque_message"] != "arbitrary-publish-content" {
		t.Fatalf("encrypted execution payload should preserve action-sensitive input: %#v", decryptedPayload)
	}
	if decryptedPayload.Payload["access_token"] != "raw-access-token" || !strings.Contains(decryptedPayload.Payload["sql"].(string), "super-secret") {
		t.Fatalf("encrypted execution payload should preserve raw action payload: %#v", decryptedPayload)
	}
	if decryptedPayload.ApprovalPreview["body"] != "password=visible-for-approval internal_abc123" {
		t.Fatalf("encrypted approval preview changed: %#v", decryptedPayload.ApprovalPreview)
	}
	if !strings.Contains(decryptedPayload.Reason, "raw-reason-token") || !strings.Contains(decryptedPayload.Reason, "reason-secret") {
		t.Fatalf("encrypted execution payload should preserve raw reason: %#v", decryptedPayload)
	}
}

func TestRunningConnectorActionResponseRedactsOutput(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
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
		Output:      map[string]any{"rows": []map[string]any{{"session_token": "raw-token", "name": "safe"}}},
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
	runtime := connectorActionTestRuntime(t, database, secretVault)
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
			"rows": []map[string]any{
				{
					"customer_secret": "visible-only-if-buggy",
					"token":           "token-value",
					"access_token":    "access-token-value",
					"password_hash":   "password-hash-value",
					"api_token_hash":  "api-token-hash-value",
					"secret_value":    "secret-value",
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
	for _, secret := range []string{"visible-only-if-buggy", "token-value", "access-token-value", "password-hash-value", "api-token-hash-value", "secret-value"} {
		if strings.Contains(outputJSON, secret) {
			t.Fatalf("structured connector output leaked sensitive field value %q: %s", secret, outputJSON)
		}
	}
	for _, marker := range []string{`"customer_secret":"[REDACTED]"`, `"token":"[REDACTED]"`, `"access_token":"[REDACTED]"`, `"password_hash":"[REDACTED]"`, `"api_token_hash":"[REDACTED]"`, `"secret_value":"[REDACTED]"`} {
		if !strings.Contains(outputJSON, marker) {
			t.Fatalf("structured connector output missing redacted marker %s: %s", marker, outputJSON)
		}
	}
	if !strings.Contains(outputJSON, `"name":"safe"`) {
		t.Fatalf("structured connector output leaked sensitive field values: %s", outputJSON)
	}
}
