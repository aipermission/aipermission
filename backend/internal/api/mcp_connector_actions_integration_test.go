package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayaccesshttp "github.com/aipermission/aipermission/backend/internal/gatewayaccess/httpowner"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type mcpConnectorActionCallRequest = mcpconnector.ActionCallRequest
type mcpConnectorActionResponse = actions.Response

func TestMCPListConnectorTargetsUsesActionPermissions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token := createAPITestToken(t, fixture, ctx, "codex")
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, testRuntimeVault(t, fixture.server, fixture.server.activeRuntime()), fixture.server.activeRuntime().Identity().WorkspaceID)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    testPostgresGetSchemasAction,
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set allowed action permission: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    testPostgresReadonlySQLAction,
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
	var items []mcpconnector.TargetItem
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one target/profile, got %#v", items)
	}
	if items[0].TargetRef != connectors.FormatTargetRef(testPostgresConnectorKind, target.ID, profile.ID) {
		t.Fatalf("target ref = %q", items[0].TargetRef)
	}
	if len(items[0].Actions) != 1 || items[0].Actions[0].Name != testPostgresGetSchemasAction {
		t.Fatalf("blocked and unsupported actions should be hidden: %#v", items[0].Actions)
	}
	if len(items[0].Hints) == 0 {
		t.Fatalf("expected connector hints")
	}

	actionsResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-actions?target_ref="+connectors.FormatTargetRef(testPostgresConnectorKind, target.ID, profile.ID), token.TokenValue, nil)
	if actionsResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", actionsResponse.Code, actionsResponse.Body.String())
	}
	if !strings.Contains(actionsResponse.Body.String(), testPostgresGetSchemasAction) ||
		!strings.Contains(actionsResponse.Body.String(), `"retry_policy":{"class":"read_only"`) ||
		strings.Contains(actionsResponse.Body.String(), testPostgresReadonlySQLAction) ||
		strings.Contains(actionsResponse.Body.String(), "no_longer_supported") {
		t.Fatalf("unexpected action discovery response: %s", actionsResponse.Body.String())
	}
}

func TestConnectorActionPollWithholdsVaultSessionOutputWithoutExactLease(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	token := createAPITestToken(t, fixture, ctx, "vault-session-poll")
	target := fixture.createKeyAndServer(t, "vault-session-output")
	runtime := fixture.server.activeRuntime()
	store := connectortargets.NewStore(fixture.db)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: target.ProfileID,
		ActionName: "exec", ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set action permission: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := fixture.db.ExecContext(ctx, `
		INSERT INTO console_sessions (
			runtime_id, generation, principal_kind, principal_token_id, workspace_id,
			runtime_instance_id, environment_content_hash, approval_context_hash,
			name, status, created_at, updated_at
		) VALUES (?, 1, 'mcp_token', ?, ?, ?, 'environment-hash', 'approval-hash', 'Vault session', 'connected', ?, ?)`,
		target.ID,
		token.ID,
		runtime.Identity().WorkspaceID,
		runtime.Identity().RuntimeID,
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
		TargetID: target.ID, ProfileID: target.ProfileID, ConnectorKind: "ssh", ActionName: "exec",
		Input: map[string]any{"secret_input": "withhold-me"}, Output: map[string]any{"secret_derived_result": "withhold-me"},
		DisplayText: "withhold-me", Error: "withhold-me",
	}
	for name, malformed := range map[string]connectortargets.ActionRequest{
		"missing session id":         {SessionGeneration: &generation},
		"missing session generation": {SessionID: &sessionID},
	} {
		t.Run(name, func(t *testing.T) {
			if gatewayaccesshttp.Authorized(ctx, testMCPOutputAuthorization(t, fixture.server, runtime, token.ID), token.ID, malformed) {
				t.Fatalf("partial Vault session handle authorized output")
			}
			response := gatewayaccesshttp.ResponseForToken(ctx, testMCPOutputAuthorization(t, fixture.server, runtime, token.ID), fixture.server.connectorRuntime.RunningHintPort(), token.ID, malformed, connectors.ActionResult{
				Status: connectors.ResultCompleted, Output: request.Output, DisplayText: request.DisplayText,
			})
			if !response.OutputWithheld || response.Input != nil || response.Output != nil || response.DisplayText != "" || response.Error != "" {
				t.Fatalf("partial Vault session handle exposed output: %#v", response)
			}
		})
	}
	if gatewayaccesshttp.Authorized(ctx, testMCPOutputAuthorization(t, fixture.server, runtime, token.ID), token.ID, request) {
		t.Fatalf("Vault session output was authorized without a lease")
	}
	withheld := gatewayaccesshttp.ResponseForToken(ctx, testMCPOutputAuthorization(t, fixture.server, runtime, token.ID), fixture.server.connectorRuntime.RunningHintPort(), token.ID, request, connectors.ActionResult{
		Status: connectors.ResultCompleted, Output: request.Output, DisplayText: request.DisplayText,
	})
	if !withheld.OutputWithheld || withheld.Input != nil || withheld.Output != nil || withheld.DisplayText != "" || withheld.Error != "" {
		t.Fatalf("completed call/replay response exposed output without a lease: %#v", withheld)
	}
	if err := testRuntimeLeases(t, fixture.server, runtime).Grant(vaultsessions.Lease{
		WorkspaceID: runtime.Identity().WorkspaceID, RuntimeInstanceID: runtime.Identity().RuntimeID,
		TokenID: token.ID, RuntimeID: target.ID, SessionID: sessionID, SessionGeneration: generation,
		EnvironmentContentHash: "environment-hash", ApprovalContextHash: "approval-hash",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("grant Vault session lease: %v", err)
	}
	principal, err := fixture.server.tokenExecutionPrincipal(runtime, token.ID)
	if err != nil {
		t.Fatalf("create token execution principal: %v", err)
	}
	if err := testRuntimeLeases(t, fixture.server, runtime).Authorize(ctx, principal, console.SessionAuthorization{
		Handle:                 console.SessionHandle{ID: sessionID, RuntimeID: target.ID, Generation: generation},
		EnvironmentContentHash: "environment-hash",
		ApprovalContextHash:    "approval-hash",
	}, console.OperationObserve); err != nil {
		t.Fatalf("exact Vault lease should authorize output: %v", err)
	}
	if !gatewayaccesshttp.Authorized(ctx, testMCPOutputAuthorization(t, fixture.server, runtime, token.ID), token.ID, request) {
		t.Fatalf("valid exact Vault lease did not authorize connector output")
	}
	authorized := gatewayaccesshttp.ResponseForToken(ctx, testMCPOutputAuthorization(t, fixture.server, runtime, token.ID), fixture.server.connectorRuntime.RunningHintPort(), token.ID, request, connectors.ActionResult{
		Status: connectors.ResultCompleted, Output: request.Output, DisplayText: request.DisplayText,
	})
	if authorized.OutputWithheld || authorized.Output == nil || authorized.DisplayText == "" {
		t.Fatalf("valid exact Vault lease did not expose connector output: %#v", authorized)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: target.ProfileID,
		ActionName: "exec", ExecutionRule: connectortargets.ActionPermissionBlocked,
	}); err != nil {
		t.Fatalf("block action permission: %v", err)
	}
	if gatewayaccesshttp.Authorized(ctx, testMCPOutputAuthorization(t, fixture.server, runtime, token.ID), token.ID, request) {
		t.Fatalf("blocked action permission still authorized stored output")
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: target.ProfileID,
		ActionName: "exec", ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("restore action permission: %v", err)
	}
	testRuntimeLeases(t, fixture.server, runtime).RevokeToken(token.ID)
	if gatewayaccesshttp.Authorized(ctx, testMCPOutputAuthorization(t, fixture.server, runtime, token.ID), token.ID, request) {
		t.Fatalf("revoked Vault lease still authorized connector output")
	}
}

func TestMCPProjectScopeHidesTargetsAndBlocksActions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token := createAPITestToken(t, fixture, ctx, "project-scoped-codex")
	projects := projectstore.NewStore(fixture.db)
	project, err := projects.Create(ctx, "Project Alpha")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, testRuntimeVault(t, fixture.server, fixture.server.activeRuntime()), fixture.server.activeRuntime().Identity().WorkspaceID)
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
		ActionName:    testPostgresGetSchemasAction,
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
	scopePath := "/api/tokens/" + strconv.FormatInt(token.ID, 10) + "/project-scopes"
	scopeResponse := performJSON(fixture.server.Handler(), http.MethodPut, scopePath, "", withCurrentAuthorizationRevision(
		t, fixture.server.Handler(), scopePath, accesscontrol.UpdateProjectScopesRequest{EnabledProjectIDs: []int64{ungrouped.ID}},
	))
	if scopeResponse.Code != http.StatusOK {
		t.Fatalf("disable project scope: %d %s", scopeResponse.Code, scopeResponse.Body.String())
	}

	hidden := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if hidden.Code != http.StatusOK || hidden.Body.String() != "[]\n" {
		t.Fatalf("disabled project should be hidden: %d %s", hidden.Code, hidden.Body.String())
	}
	action := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef:      connectors.FormatTargetRef(testPostgresConnectorKind, target.ID, profile.ID),
		ActionName:     testPostgresGetSchemasAction,
		Reason:         "verify disabled project scope",
		IdempotencyKey: "disabled-project-scope",
	})
	unknown := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef:      connectors.FormatTargetRef(testPostgresConnectorKind, target.ID+9999, profile.ID+9999),
		ActionName:     testPostgresGetSchemasAction,
		Reason:         "verify unknown target",
		IdempotencyKey: "unknown-project-scope",
	})
	if action.Code != http.StatusNotFound || unknown.Code != action.Code || unknown.Body.String() != action.Body.String() {
		t.Fatalf("hidden and unknown targets must be indistinguishable: hidden=%d %s unknown=%d %s", action.Code, action.Body.String(), unknown.Code, unknown.Body.String())
	}
	var requestCount int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_action_requests WHERE token_id = ?`, token.ID).Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if requestCount != 0 {
		t.Fatalf("invisible target attempts persisted %d requests", requestCount)
	}

	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    testPostgresReadonlySQLAction,
		ExecutionRule: connectortargets.ActionPermissionBlocked,
	}); err != nil {
		t.Fatalf("set blocked action permission: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `UPDATE token_project_scopes SET enabled = 1 WHERE token_id = ? AND project_id = ?`, token.ID, project.ID); err != nil {
		t.Fatalf("restore project scope: %v", err)
	}
	unauthorizedAction := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef:      connectors.FormatTargetRef(testPostgresConnectorKind, target.ID, profile.ID),
		ActionName:     testPostgresReadonlySQLAction,
		Reason:         "verify unauthorized action",
		IdempotencyKey: "unauthorized-action-scope",
	})
	if unauthorizedAction.Code != unknown.Code || unauthorizedAction.Body.String() != unknown.Body.String() {
		t.Fatalf("unauthorized action and unknown target must be indistinguishable: unauthorized=%d %s unknown=%d %s", unauthorizedAction.Code, unauthorizedAction.Body.String(), unknown.Code, unknown.Body.String())
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_action_requests WHERE token_id = ?`, token.ID).Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if requestCount != 0 {
		t.Fatalf("unauthorized action attempts persisted %d requests", requestCount)
	}
}

func TestMCPConnectorActionIdempotencyReplaysAndRejectsDrift(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	token := createAPITestToken(t, fixture, ctx, "idempotent-connector")
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, testRuntimeVault(t, fixture.server, fixture.server.activeRuntime()), fixture.server.activeRuntime().Identity().WorkspaceID)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: profile.ID,
		ActionName:    testPostgresGetSchemasAction,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set permission: %v", err)
	}
	request := mcpConnectorActionCallRequest{
		TargetRef:  connectors.FormatTargetRef(testPostgresConnectorKind, target.ID, profile.ID),
		ActionName: testPostgresGetSchemasAction, Reason: "inspect schema",
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
	if firstResponse.RetryPolicy.Class != connectors.RetryReadOnly || secondResponse.RetryPolicy.Class != firstResponse.RetryPolicy.Class || secondResponse.RetryPolicy.Guidance != firstResponse.RetryPolicy.Guidance {
		t.Fatalf("retry policy did not survive call/replay: first=%#v second=%#v", firstResponse.RetryPolicy, secondResponse.RetryPolicy)
	}
	poll := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-action-requests/"+strconv.FormatInt(firstResponse.RequestID, 10), token.TokenValue, nil)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), `"retry_policy":{"class":"read_only"`) {
		t.Fatalf("poll retry policy: %d %s", poll.Code, poll.Body.String())
	}
	approval := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals/"+strconv.FormatInt(firstResponse.RequestID, 10), "", nil)
	if approval.Code != http.StatusOK || !strings.Contains(approval.Body.String(), `"retry_policy":{"class":"read_only"`) {
		t.Fatalf("approval retry policy: %d %s", approval.Code, approval.Body.String())
	}
	var historyPolicy string
	if err := fixture.db.QueryRow(`SELECT retry_policy_json FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, firstResponse.RequestID).Scan(&historyPolicy); err != nil {
		t.Fatalf("read history retry policy: %v", err)
	}
	if !strings.Contains(historyPolicy, `"class":"read_only"`) {
		t.Fatalf("history retry policy = %s", historyPolicy)
	}
	historyResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?target_id="+strconv.FormatInt(target.ID, 10)+"&limit=10", "", nil)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("history response: %d %s", historyResponse.Code, historyResponse.Body.String())
	}
	historyPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, historyResponse.Body.Bytes())
	responsePolicy := ""
	for _, item := range historyPage.Items {
		if item.SourceRefType == "connector_action_request" && item.SourceRefID == firstResponse.RequestID {
			responsePolicy = item.RetryPolicyJSON
			break
		}
	}
	if !strings.Contains(responsePolicy, `"class":"read_only"`) {
		t.Fatalf("history response retry policy = %#v", historyPage.Items)
	}
	if secondResponse.Error != "Waiting for user approval." {
		t.Fatalf("approval replay error = %q", secondResponse.Error)
	}
	refreshedAttempt := request
	refreshedAttempt.Reason = "inspect schema after refreshing external state"
	sameKey := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, refreshedAttempt)
	if sameKey.Code != http.StatusConflict {
		t.Fatalf("changed logical attempt with old key: %d %s", sameKey.Code, sameKey.Body.String())
	}
	refreshedAttempt.IdempotencyKey = "connector-request-2"
	newKey := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, refreshedAttempt)
	if newKey.Code != http.StatusOK || !strings.Contains(newKey.Body.String(), `"status":"approval_pending"`) {
		t.Fatalf("refreshed logical attempt with new key: %d %s", newKey.Code, newKey.Body.String())
	}
	testRuntimeControlState(t, fixture.server, fixture.server.activeRuntime()).SetMCPStarted(false)
	stoppedReplay := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, request)
	if stoppedReplay.Code != http.StatusOK || !strings.Contains(stoppedReplay.Body.String(), `"status":"stopped"`) {
		t.Fatalf("stopped MCP replay: %d %s", stoppedReplay.Code, stoppedReplay.Body.String())
	}
	stoppedPoll := performJSON(fixture.server.Handler(), http.MethodGet, fmt.Sprintf("/api/mcp/connector-action-requests/%d", firstResponse.RequestID), token.TokenValue, nil)
	if stoppedPoll.Code != http.StatusOK || !strings.Contains(stoppedPoll.Body.String(), `"output_withheld":true`) {
		t.Fatalf("stopped MCP poll: %d %s", stoppedPoll.Code, stoppedPoll.Body.String())
	}
	newRequest := request
	newRequest.IdempotencyKey = "connector-request-while-stopped"
	stoppedNew := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, newRequest)
	if stoppedNew.Code != http.StatusOK || !strings.Contains(stoppedNew.Body.String(), `"status":"stopped"`) {
		t.Fatalf("stopped MCP new call: %d %s", stoppedNew.Code, stoppedNew.Body.String())
	}
	testRuntimeControlState(t, fixture.server, fixture.server.activeRuntime()).SetMCPStarted(true)
	if _, err := fixture.db.Exec(`UPDATE connector_targets SET status = 'archived' WHERE id = ?`, target.ID); err != nil {
		t.Fatalf("archive target: %v", err)
	}
	archivedReplay := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, request)
	if archivedReplay.Code != http.StatusOK || !strings.Contains(archivedReplay.Body.String(), `"replayed":true`) ||
		!strings.Contains(archivedReplay.Body.String(), `"output_withheld":true`) {
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

func TestMCPConnectorActionAllowsLegacyReadsButRequiresIdempotencyForMutations(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	token, err := fixture.tokens.Create(t.Context(), tokens.CreateRequest{Name: "missing-idempotency"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, testRuntimeVault(t, fixture.server, fixture.server.activeRuntime()), fixture.server.activeRuntime().Identity().WorkspaceID)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: profile.ID,
		ActionName: testPostgresGetSchemasAction, ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set read permission: %v", err)
	}
	readResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef: connectors.FormatTargetRef(testPostgresConnectorKind, target.ID, profile.ID), ActionName: testPostgresGetSchemasAction,
	})
	if readResponse.Code != http.StatusOK || !strings.Contains(readResponse.Body.String(), `"status":"approval_pending"`) {
		t.Fatalf("legacy read without idempotency key = %d %s", readResponse.Code, readResponse.Body.String())
	}

	sshProfile := fixture.createKeyAndServer(t, "missing-idempotency-mutation")
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: sshProfile.TargetID, ProfileID: sshProfile.ProfileID,
		ActionName: "exec", ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set mutation permission: %v", err)
	}
	mutationResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef: sshProfile.TargetRef, ActionName: "exec", Input: map[string]any{"command": "true"},
	})
	if mutationResponse.Code != http.StatusBadRequest || !strings.Contains(mutationResponse.Body.String(), "idempotency_key is required for connector mutations") {
		t.Fatalf("mutation without idempotency key = %d %s", mutationResponse.Code, mutationResponse.Body.String())
	}
}

func TestMCPConnectorActionOutcomeUnknownForbidsAutomaticRetry(t *testing.T) {
	response := mcpconnector.ResponseFromRequest(nil, connectortargets.ActionRequest{
		ID: 42, Status: connectors.ResultOutcomeUnknown,
		ConnectorKind: "ssh", TargetID: 7, ProfileID: 8, ActionName: "exec",
	})
	if response.RetryAfterSeconds != 0 {
		t.Fatalf("outcome_unknown retry_after_seconds = %d", response.RetryAfterSeconds)
	}
	if !strings.Contains(response.AssistantHint, "Do not retry") || !strings.Contains(response.AssistantHint, "connector-specific read-only action") || strings.Contains(response.AssistantHint, "read_console") {
		t.Fatalf("outcome_unknown assistant hint = %q", response.AssistantHint)
	}
}

func TestMCPConnectorActionWithheldOutputPreservesNoRetryGuidance(t *testing.T) {
	sessionID := int64(41)
	request := connectortargets.ActionRequest{
		ID: 42, Status: connectors.ResultOutcomeUnknown,
		ConnectorKind: "ssh", TargetID: 7, ProfileID: 8, ActionName: "exec",
		SessionID: &sessionID,
		Output:    map[string]any{"secret_derived_result": "withhold-me"},
	}
	response := gatewayaccesshttp.ResponseForToken(context.Background(), &gatewayaccess.MCPOutputAuthorization{}, nil, 9, request, connectors.ActionResult{
		Status: connectors.ResultOutcomeUnknown,
		Output: request.Output,
	})
	if !response.OutputWithheld || response.Output != nil {
		t.Fatalf("response did not withhold output: %#v", response)
	}
	if response.TargetRef != "" || response.TargetName != "" || response.ConnectorKind != "" || response.ProfileLabel != "" {
		t.Fatalf("response retained target/profile identity: %#v", response)
	}
	if !strings.Contains(response.AssistantHint, "Do not retry") || !strings.Contains(response.AssistantHint, "authorization") {
		t.Fatalf("withheld response lost safety guidance: %q", response.AssistantHint)
	}
}

func TestMCPConnectorActionResponseWriteFencesTokenRevocation(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	token := createAPITestToken(t, fixture, ctx, "response-fence")
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, testRuntimeVault(t, fixture.server, fixture.server.activeRuntime()), fixture.server.activeRuntime().Identity().WorkspaceID)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: profile.ID,
		ActionName: testPostgresGetSchemasAction, ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set permission: %v", err)
	}
	runtime := fixture.server.activeRuntime()
	testRuntimeControlState(t, fixture.server, runtime).SetMCPStarted(true)
	request := connectortargets.ActionRequest{
		ID: 1, TokenID: &token.ID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: testPostgresConnectorKind, ActionName: testPostgresGetSchemasAction,
	}
	response := mcpconnector.ResponseFromRequest(fixture.server.connectorRuntime.RunningHintPort(), request)
	response.Output = map[string]any{"sensitive": "bounded-result"}
	w := newBlockingResponseWriter()
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		gatewayaccesshttp.Deliver(
			w,
			httptest.NewRequest(http.MethodGet, "/api/mcp/connector-action-requests/1", nil),
			testMCPOutputAuthorization(t, fixture.server, runtime, token.ID),
			token.ID,
			request,
			response,
		)
	}()
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("response writer was not reached")
	}
	revokeDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		revokeDone <- performJSON(fixture.server.Handler(), http.MethodPost, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/revoke", "", nil)
	}()
	select {
	case <-revokeDone:
		t.Fatal("token revocation crossed an in-progress authorized response write")
	case <-time.After(50 * time.Millisecond):
	}
	close(w.release)
	<-writeDone
	revoked := <-revokeDone
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke token: %d %s", revoked.Code, revoked.Body.String())
	}
	if !strings.Contains(w.body.String(), "bounded-result") {
		t.Fatalf("authorized response was not written before revocation: %s", w.body.String())
	}

	after := httptest.NewRecorder()
	gatewayaccesshttp.Deliver(
		after,
		httptest.NewRequest(http.MethodGet, "/api/mcp/connector-action-requests/1", nil),
		testMCPOutputAuthorization(t, fixture.server, runtime, token.ID),
		token.ID,
		request,
		response,
	)
	if strings.Contains(after.Body.String(), "bounded-result") || !strings.Contains(after.Body.String(), `"output_withheld":true`) {
		t.Fatalf("revoked response crossed the write boundary: %s", after.Body.String())
	}
}

type blockingResponseWriter struct {
	header  http.Header
	body    bytes.Buffer
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingResponseWriter() *blockingResponseWriter {
	return &blockingResponseWriter{header: make(http.Header), entered: make(chan struct{}), release: make(chan struct{})}
}

func (w *blockingResponseWriter) Header() http.Header { return w.header }
func (w *blockingResponseWriter) WriteHeader(int)     {}
func (w *blockingResponseWriter) Write(value []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return w.body.Write(value)
}
