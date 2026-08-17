package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestMCPListConnectorTargetsUsesActionPermissions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionGetSchemas,
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set allowed action permission: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionBlocked,
	}); err != nil {
		t.Fatalf("set blocked action permission: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "no_longer_supported",
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set unsupported action permission: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var items []mcpConnectorTargetItem
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one target/profile, got %#v", items)
	}
	if items[0].TargetRef != connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID) {
		t.Fatalf("target ref = %q", items[0].TargetRef)
	}
	if len(items[0].Actions) != 1 || items[0].Actions[0].Name != postgresconnector.ActionGetSchemas {
		t.Fatalf("blocked and unsupported actions should be hidden: %#v", items[0].Actions)
	}
	if len(items[0].Hints) == 0 {
		t.Fatalf("expected connector hints")
	}

	actionsResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-actions?target_ref="+connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID), token.TokenValue, nil)
	if actionsResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", actionsResponse.Code, actionsResponse.Body.String())
	}
	if !strings.Contains(actionsResponse.Body.String(), postgresconnector.ActionGetSchemas) ||
		strings.Contains(actionsResponse.Body.String(), postgresconnector.ActionQueryReadonly) ||
		strings.Contains(actionsResponse.Body.String(), "no_longer_supported") {
		t.Fatalf("unexpected action discovery response: %s", actionsResponse.Body.String())
	}
}

func TestConnectorActionPollWithholdsVaultSessionOutputWithoutExactLease(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-session-poll"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target := fixture.createKeyAndServer(t, "vault-session-output")
	runtime := fixture.server.activeRuntime()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := fixture.db.ExecContext(ctx, `
		INSERT INTO console_sessions (
			runtime_id, generation, principal_kind, principal_token_id, workspace_id,
			runtime_instance_id, environment_content_hash, approval_context_hash,
			name, status, created_at, updated_at
		) VALUES (?, 1, 'mcp_token', ?, ?, ?, 'environment-hash', 'approval-hash', 'Vault session', 'connected', ?, ?)`,
		target.ID,
		token.ID,
		runtime.workspaceUUID,
		runtime.runtimeInstanceID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert Vault console session: %v", err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read Vault console session id: %v", err)
	}
	generation := int64(1)
	request := connectortargets.ActionRequest{
		TokenID: &token.ID, SessionID: &sessionID, SessionGeneration: &generation,
	}
	if connectorActionVaultPollAuthorized(ctx, runtime, token.ID, request) {
		t.Fatalf("Vault session output was authorized without a lease")
	}
	if err := runtime.vaultLeases.Grant(vaultsessions.Lease{
		WorkspaceID: runtime.workspaceUUID, RuntimeInstanceID: runtime.runtimeInstanceID,
		TokenID: token.ID, RuntimeID: target.ID, SessionID: sessionID, SessionGeneration: generation,
		EnvironmentContentHash: "environment-hash", ApprovalContextHash: "approval-hash",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("grant Vault session lease: %v", err)
	}
	principal, err := tokenExecutionPrincipal(runtime, token.ID)
	if err != nil {
		t.Fatalf("create token execution principal: %v", err)
	}
	if err := runtime.vaultLeases.Authorize(ctx, principal, console.SessionAuthorization{
		Handle:                 console.SessionHandle{ID: sessionID, RuntimeID: target.ID, Generation: generation},
		EnvironmentContentHash: "environment-hash",
		ApprovalContextHash:    "approval-hash",
	}, console.OperationObserve); err != nil {
		t.Fatalf("exact Vault lease should authorize output: %v", err)
	}
	if !connectorActionVaultPollAuthorized(ctx, runtime, token.ID, request) {
		t.Fatalf("valid exact Vault lease did not authorize connector output")
	}
	runtime.vaultLeases.RevokeToken(token.ID)
	if connectorActionVaultPollAuthorized(ctx, runtime, token.ID, request) {
		t.Fatalf("revoked Vault lease still authorized connector output")
	}
}

func TestMCPProjectScopeHidesTargetsAndBlocksActions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "project-scoped-codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	projects := projectstore.NewStore(fixture.db)
	project, err := projects.Create(ctx, "Project Alpha")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	target, err = store.UpdateTarget(ctx, connectortargets.UpdateTargetInput{
		ID:        target.ID,
		ProjectID: project.ID,
		Name:      target.Name,
		Config:    target.Config,
	})
	if err != nil {
		t.Fatalf("move target to project: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionGetSchemas,
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set action permission: %v", err)
	}

	visible := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if visible.Code != http.StatusOK || !strings.Contains(visible.Body.String(), `"project_name":"Project Alpha"`) {
		t.Fatalf("project target should be visible: %d %s", visible.Code, visible.Body.String())
	}

	ungrouped, err := projects.Ungrouped(ctx)
	if err != nil {
		t.Fatalf("get ungrouped project: %v", err)
	}
	scopeResponse := performJSON(fixture.server.Handler(), http.MethodPut, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-scopes", "", updateTokenProjectScopesRequest{EnabledProjectIDs: []int64{ungrouped.ID}})
	if scopeResponse.Code != http.StatusOK {
		t.Fatalf("disable project scope: %d %s", scopeResponse.Code, scopeResponse.Body.String())
	}

	hidden := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if hidden.Code != http.StatusOK || hidden.Body.String() != "[]\n" {
		t.Fatalf("disabled project should be hidden: %d %s", hidden.Code, hidden.Body.String())
	}
	action := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionGetSchemas,
		Reason:     "verify disabled project scope",
	})
	if action.Code != http.StatusOK || !strings.Contains(action.Body.String(), `"status":"blocked"`) {
		t.Fatalf("disabled project action should be blocked: %d %s", action.Code, action.Body.String())
	}
}

func TestMCPConnectorActionIdempotencyReplaysAndRejectsDrift(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "idempotent-connector"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: profile.ID,
		ActionName:    postgresconnector.ActionGetSchemas,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set permission: %v", err)
	}
	request := mcpConnectorActionCallRequest{
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionGetSchemas, Reason: "inspect schema",
		IdempotencyKey: "connector-request-1",
	}
	first := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first call: %d %s", first.Code, first.Body.String())
	}
	var firstResponse mcpConnectorActionResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResponse); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	second := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, request)
	if second.Code != http.StatusOK {
		t.Fatalf("replay call: %d %s", second.Code, second.Body.String())
	}
	var secondResponse mcpConnectorActionResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondResponse); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if secondResponse.RequestID != firstResponse.RequestID || !secondResponse.Replayed || secondResponse.Status != string(connectors.ResultApprovalPending) {
		t.Fatalf("unexpected replay: first=%#v second=%#v", firstResponse, secondResponse)
	}
	if secondResponse.Error != "Waiting for user approval." {
		t.Fatalf("approval replay error = %q", secondResponse.Error)
	}
	fixture.server.activeRuntime().setMCPStarted(false)
	stoppedReplay := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, request)
	if stoppedReplay.Code != http.StatusOK || !strings.Contains(stoppedReplay.Body.String(), `"replayed":true`) {
		t.Fatalf("stopped MCP replay: %d %s", stoppedReplay.Code, stoppedReplay.Body.String())
	}
	newRequest := request
	newRequest.IdempotencyKey = "connector-request-while-stopped"
	stoppedNew := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, newRequest)
	if stoppedNew.Code != http.StatusOK || !strings.Contains(stoppedNew.Body.String(), `"status":"stopped"`) {
		t.Fatalf("stopped MCP new call: %d %s", stoppedNew.Code, stoppedNew.Body.String())
	}
	fixture.server.activeRuntime().setMCPStarted(true)
	if _, err := fixture.db.Exec(`UPDATE connector_targets SET status = 'archived' WHERE id = ?`, target.ID); err != nil {
		t.Fatalf("archive target: %v", err)
	}
	archivedReplay := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, request)
	if archivedReplay.Code != http.StatusOK || !strings.Contains(archivedReplay.Body.String(), `"replayed":true`) {
		t.Fatalf("archived target replay: %d %s", archivedReplay.Code, archivedReplay.Body.String())
	}
	request.Reason = "different reason"
	conflict := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, request)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("drift status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_action_requests WHERE token_id = ? AND idempotency_key = ?`, token.ID, request.IdempotencyKey).Scan(&count); err != nil {
		t.Fatalf("count requests: %v", err)
	}
	if count != 1 {
		t.Fatalf("request count=%d", count)
	}
}

func TestMCPConnectorActionOutcomeUnknownForbidsAutomaticRetry(t *testing.T) {
	response := connectorActionRequestToMCPResponse(connectortargets.ActionRequest{
		ID: 42, Status: connectors.ResultOutcomeUnknown,
		ConnectorKind: "ssh", TargetID: 7, ProfileID: 8, ActionName: "exec",
	})
	if response.RetryAfterSeconds != 0 {
		t.Fatalf("outcome_unknown retry_after_seconds = %d", response.RetryAfterSeconds)
	}
	if !strings.Contains(response.AssistantHint, "Do not retry") || !strings.Contains(response.AssistantHint, "read_console") {
		t.Fatalf("outcome_unknown assistant hint = %q", response.AssistantHint)
	}
}
