package accesscontrol

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const (
	testConnectorKind = "access_test"
	testActionName    = "inspect"
)

type accessTestConnector struct{ actionCount int }

func (accessTestConnector) Kind() string                    { return testConnectorKind }
func (accessTestConnector) Label() string                   { return "Access test" }
func (accessTestConnector) Version() string                 { return "0.1" }
func (accessTestConnector) TargetSchema() connectors.Schema { return connectors.Schema{} }
func (accessTestConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{Kind: "operator", Label: "Operator"}}
}
func (accessTestConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{Connector: testConnectorKind, ConnectorID: testConnectorKind}, nil
}
func (c accessTestConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	if c.actionCount < 2 {
		return []connectors.ActionDefinition{{Name: testActionName, Label: "Inspect", Description: "Inspect test state.", Risk: connectors.RiskRead}}, nil
	}
	actions := make([]connectors.ActionDefinition, 0, c.actionCount)
	for index := range c.actionCount {
		actions = append(actions, connectors.ActionDefinition{Name: fmt.Sprintf("inspect_%02d", index), Label: "Inspect", Description: "Inspect test state.", Risk: connectors.RiskRead})
	}
	return actions, nil
}
func (accessTestConnector) PrepareAction(context.Context, connectors.ActionRequest) (connectors.PreparedAction, error) {
	return connectors.PreparedAction{ConnectorKind: testConnectorKind, ActionName: testActionName}, nil
}
func (accessTestConnector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
}

type handlerFixture struct {
	database      *sql.DB
	tokenID       int64
	projectID     int64
	targetID      int64
	profileID     int64
	mux           *http.ServeMux
	invalidations int
}

func newHandlerFixture(t *testing.T) *handlerFixture {
	return newHandlerFixtureWithOptions(t, 1, false)
}

func newHandlerFixtureWithOptions(t *testing.T, actionCount int, realAudit bool) *handlerFixture {
	t.Helper()
	database := openTestDatabase(t)
	project, err := projects.NewStore(database).Create(t.Context(), "Access project")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "access agent"})
	if err != nil {
		t.Fatal(err)
	}
	targets := connectortargets.NewStore(database)
	target, err := targets.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ProjectID: project.ID, ConnectorKind: testConnectorKind, Name: "Access target",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targets.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: testConnectorKind, Kind: "operator", Label: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := connectors.NewRegistry()
	if err := registry.Register(accessTestConnector{actionCount: actionCount}); err != nil {
		t.Fatal(err)
	}
	runner := auditRunner(database, nil)
	if realAudit {
		coordinator := observability.NewCoordinator(database, nil, nil, nil)
		runner = func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return coordinator.WithMutation(ctx, "user", &token.ID, 0, action, payload, mutate)
		}
	}
	fixture := &handlerFixture{
		database: database, tokenID: token.ID, projectID: project.ID,
		targetID: target.ID, profileID: profile.ID, mux: http.NewServeMux(),
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Database: database, Tokens: tokens.NewStore(database), Registry: registry,
			ReusableTokens:   func(context.Context) (bool, error) { return false, nil },
			Mutate:           runner,
			AcquireExclusive: func(context.Context) (func(), error) { return func() {}, nil },
			FinishTokenInvalidation: func(context.Context, int64, []int64) {
				fixture.invalidations++
			},
		}, true
	})
	fixture.mux.HandleFunc("GET /tokens", handlers.ListTokens)
	fixture.mux.HandleFunc("POST /tokens/{id}/revoke", handlers.RevokeToken)
	fixture.mux.HandleFunc("GET /tokens/{id}/connector-permissions", handlers.ListConnectorPermissions)
	fixture.mux.HandleFunc("PUT /tokens/{id}/connector-permissions", handlers.UpdateConnectorPermissions)
	fixture.mux.HandleFunc("GET /tokens/{id}/project-scopes", handlers.ListProjectScopes)
	fixture.mux.HandleFunc("PUT /tokens/{id}/project-scopes", handlers.UpdateProjectScopes)
	fixture.mux.HandleFunc("GET /tokens/{id}/project-capabilities", handlers.ListProjectCapabilities)
	fixture.mux.HandleFunc("PUT /tokens/{id}/project-capabilities", handlers.UpdateProjectCapabilities)
	return fixture
}

func TestAuthorizationHTTPHandlersOwnLifecycleAndOptimisticConcurrency(t *testing.T) {
	fixture := newHandlerFixture(t)
	base := "/tokens/" + strconv.FormatInt(fixture.tokenID, 10)

	list := performAccessRequest(t, fixture.mux, http.MethodGet, "/tokens", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"name":"access agent"`) {
		t.Fatalf("list tokens: %d %s", list.Code, list.Body.String())
	}

	scopePath := base + "/project-scopes"
	scopeRevision := responseRevision(t, fixture.mux, scopePath)
	scopeUpdate := performAccessRequest(t, fixture.mux, http.MethodPut, scopePath, UpdateProjectScopesRequest{
		EnabledProjectIDs: []int64{fixture.projectID}, ExpectedRevision: scopeRevision,
	})
	assertChangedResponse(t, scopeUpdate, true)
	scopeNoop := performAccessRequest(t, fixture.mux, http.MethodPut, scopePath, UpdateProjectScopesRequest{
		EnabledProjectIDs: []int64{fixture.projectID}, ExpectedRevision: responseRevision(t, fixture.mux, scopePath),
	})
	assertChangedResponse(t, scopeNoop, false)

	capabilityPath := base + "/project-capabilities"
	capabilityUpdate := performAccessRequest(t, fixture.mux, http.MethodPut, capabilityPath, UpdateProjectCapabilitiesRequest{
		Capabilities: []ProjectCapabilityInput{{
			ProjectID: fixture.projectID, CapabilityName: VaultMetadataRead, ExecutionRule: RuleAlwaysRun,
		}},
		ExpectedRevision: responseRevision(t, fixture.mux, capabilityPath),
	})
	assertChangedResponse(t, capabilityUpdate, true)

	permissionPath := base + "/connector-permissions"
	staleRevision := responseRevision(t, fixture.mux, permissionPath)
	permission := ConnectorPermissionInput{
		TargetID: fixture.targetID, ProfileID: fixture.profileID,
		ActionName: testActionName, ExecutionRule: string(connectortargets.ActionPermissionAlwaysRun),
	}
	permissionUpdate := performAccessRequest(t, fixture.mux, http.MethodPut, permissionPath, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{permission}, ExpectedRevision: staleRevision,
	})
	assertChangedResponse(t, permissionUpdate, true)
	conflict := performAccessRequest(t, fixture.mux, http.MethodPut, permissionPath, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{permission}, ExpectedRevision: staleRevision,
	})
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "changed in another client") {
		t.Fatalf("stale permission update: %d %s", conflict.Code, conflict.Body.String())
	}
	permissionNoop := performAccessRequest(t, fixture.mux, http.MethodPut, permissionPath, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{permission}, ExpectedRevision: responseRevision(t, fixture.mux, permissionPath),
	})
	assertChangedResponse(t, permissionNoop, false)

	revokePath := base + "/revoke"
	firstRevoke := performAccessRequest(t, fixture.mux, http.MethodPost, revokePath, nil)
	if firstRevoke.Code != http.StatusOK || !strings.Contains(firstRevoke.Body.String(), `"revoked_at"`) {
		t.Fatalf("revoke token: %d %s", firstRevoke.Code, firstRevoke.Body.String())
	}
	secondRevoke := performAccessRequest(t, fixture.mux, http.MethodPost, revokePath, nil)
	if secondRevoke.Code != http.StatusOK {
		t.Fatalf("repeat revoke: %d %s", secondRevoke.Code, secondRevoke.Body.String())
	}
	if fixture.invalidations != 4 {
		t.Fatalf("live invalidations = %d, want one per changed authorization", fixture.invalidations)
	}
	var revokeAudits int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'token.revoked'`).Scan(&revokeAudits); err != nil {
		t.Fatal(err)
	}
	if revokeAudits != 1 {
		t.Fatalf("revoke audits = %d, want 1", revokeAudits)
	}
}

func TestAuthorizationChangesPermanentlyStalePendingConnectorApprovals(t *testing.T) {
	fixture := newHandlerFixture(t)
	base := "/tokens/" + strconv.FormatInt(fixture.tokenID, 10)
	permissionPath := base + "/connector-permissions"
	scopePath := base + "/project-scopes"
	prompt := ConnectorPermissionInput{
		TargetID: fixture.targetID, ProfileID: fixture.profileID,
		ActionName: testActionName, ExecutionRule: string(connectortargets.ActionPermissionApprovalRequired),
	}
	blocked := prompt
	blocked.ExecutionRule = string(connectortargets.ActionPermissionBlocked)

	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, scopePath, UpdateProjectScopesRequest{
		EnabledProjectIDs: []int64{fixture.projectID}, ExpectedRevision: responseRevision(t, fixture.mux, scopePath),
	}), true)
	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, permissionPath, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{prompt}, ExpectedRevision: responseRevision(t, fixture.mux, permissionPath),
	}), true)

	permissionRequestID := insertPendingConnectorApproval(t, fixture, "permission-revoke")
	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, permissionPath, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{blocked}, ExpectedRevision: responseRevision(t, fixture.mux, permissionPath),
	}), true)
	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, permissionPath, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{prompt}, ExpectedRevision: responseRevision(t, fixture.mux, permissionPath),
	}), true)
	assertConnectorApprovalStatus(t, fixture.database, permissionRequestID, connectors.ResultStale)

	scopeRequestID := insertPendingConnectorApproval(t, fixture, "project-visibility")
	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, scopePath, UpdateProjectScopesRequest{
		EnabledProjectIDs: []int64{}, ExpectedRevision: responseRevision(t, fixture.mux, scopePath),
	}), true)
	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, scopePath, UpdateProjectScopesRequest{
		EnabledProjectIDs: []int64{fixture.projectID}, ExpectedRevision: responseRevision(t, fixture.mux, scopePath),
	}), true)
	assertConnectorApprovalStatus(t, fixture.database, scopeRequestID, connectors.ResultStale)

	predispatchRequestID := insertPendingConnectorApproval(t, fixture, "predispatch-authorization")
	store := connectortargets.NewStore(fixture.database)
	leaseExpiry := time.Now().UTC().Add(time.Minute)
	if _, err := store.MarkActionRequestRunning(t.Context(), predispatchRequestID, "approval-worker", leaseExpiry); err != nil {
		t.Fatal(err)
	}
	dispatchedRequestID := insertPendingConnectorApproval(t, fixture, "dispatched-authorization")
	if _, err := store.MarkActionRequestRunning(t.Context(), dispatchedRequestID, "approval-worker", leaseExpiry); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginActionRequestDispatch(t.Context(), dispatchedRequestID, "approval-worker", time.Now().UTC(), leaseExpiry); err != nil {
		t.Fatal(err)
	}
	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, permissionPath, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{blocked}, ExpectedRevision: responseRevision(t, fixture.mux, permissionPath),
	}), true)
	assertConnectorApprovalStatus(t, fixture.database, predispatchRequestID, connectors.ResultStale)
	assertConnectorApprovalStatus(t, fixture.database, dispatchedRequestID, connectors.ResultRunning)
}

func TestIdenticalAuthorizationUpdatePreservesPendingConnectorApproval(t *testing.T) {
	fixture := newHandlerFixture(t)
	path := "/tokens/" + strconv.FormatInt(fixture.tokenID, 10) + "/connector-permissions"
	prompt := ConnectorPermissionInput{
		TargetID: fixture.targetID, ProfileID: fixture.profileID,
		ActionName: testActionName, ExecutionRule: string(connectortargets.ActionPermissionApprovalRequired),
	}
	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, path, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{prompt}, ExpectedRevision: responseRevision(t, fixture.mux, path),
	}), true)
	requestID := insertPendingConnectorApproval(t, fixture, "no-op")
	assertChangedResponse(t, performAccessRequest(t, fixture.mux, http.MethodPut, path, UpdateConnectorPermissionsRequest{
		Permissions: []ConnectorPermissionInput{prompt}, ExpectedRevision: responseRevision(t, fixture.mux, path),
	}), false)
	assertConnectorApprovalStatus(t, fixture.database, requestID, connectors.ResultApprovalPending)
}

func insertPendingConnectorApproval(t *testing.T, fixture *handlerFixture, key string) int64 {
	t.Helper()
	tokenID := fixture.tokenID
	request, err := connectortargets.NewStore(fixture.database).InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TokenID: &tokenID, TargetID: fixture.targetID, ProfileID: fixture.profileID,
		ConnectorKind: testConnectorKind, ActionName: testActionName, Source: "mcp",
		Status: connectors.ResultApprovalPending, EncryptedPayloadJSON: "sealed-" + key,
		ApprovalContext: `{"permission":"prompt"}`, ApprovalContextHash: "approval-" + key,
	})
	if err != nil {
		t.Fatalf("insert pending connector approval: %v", err)
	}
	return request.ID
}

func assertConnectorApprovalStatus(t *testing.T, database *sql.DB, requestID int64, want connectors.ResultStatus) {
	t.Helper()
	var requestStatus, historyStatus string
	if err := database.QueryRow(`SELECT status FROM connector_action_requests WHERE id = ?`, requestID).Scan(&requestStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		SELECT status FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, requestID).Scan(&historyStatus); err != nil {
		t.Fatal(err)
	}
	wantHistory := string(want)
	if want == connectors.ResultApprovalPending {
		wantHistory = "pending_approval"
	}
	if requestStatus != string(want) || historyStatus != wantHistory {
		t.Fatalf("connector approval %d: request=%q history=%q want=%q/%q", requestID, requestStatus, historyStatus, want, wantHistory)
	}
}

func TestAuthorizationHTTPHandlersRejectInvalidInputsBeforeMutation(t *testing.T) {
	fixture := newHandlerFixture(t)
	base := "/tokens/" + strconv.FormatInt(fixture.tokenID, 10)
	permissionPath := base + "/connector-permissions"
	tests := []struct {
		name string
		path string
		body any
	}{
		{
			name: "missing revision", path: base + "/project-scopes",
			body: UpdateProjectScopesRequest{EnabledProjectIDs: []int64{fixture.projectID}},
		},
		{
			name: "unknown action", path: permissionPath,
			body: UpdateConnectorPermissionsRequest{
				ExpectedRevision: responseRevision(t, fixture.mux, permissionPath),
				Permissions: []ConnectorPermissionInput{{
					TargetID: fixture.targetID, ProfileID: fixture.profileID,
					ActionName: "unknown", ExecutionRule: string(connectortargets.ActionPermissionAlwaysRun),
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performAccessRequest(t, fixture.mux, http.MethodPut, test.path, test.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("response: %d %s", response.Code, response.Body.String())
			}
		})
	}
	if fixture.invalidations != 0 || countRows(t, fixture.database, "audit_logs") != 0 {
		t.Fatalf("invalid request mutated state: invalidations=%d", fixture.invalidations)
	}
}

func TestLargeConnectorPermissionUpdateFitsRealAuditOutbox(t *testing.T) {
	const actionCount, targetCount = 16, 12
	fixture := newHandlerFixtureWithOptions(t, actionCount, true)
	store := connectortargets.NewStore(fixture.database)
	pairs := [][2]int64{{fixture.targetID, fixture.profileID}}
	for index := 1; index < targetCount; index++ {
		target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
			ProjectID: fixture.projectID, ConnectorKind: testConnectorKind, Name: fmt.Sprintf("review-target-%02d", index),
		})
		if err != nil {
			t.Fatal(err)
		}
		profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
			TargetID: target.ID, ConnectorKind: testConnectorKind, Kind: "operator", Label: "operator",
		})
		if err != nil {
			t.Fatal(err)
		}
		pairs = append(pairs, [2]int64{target.ID, profile.ID})
	}
	inputs := make([]ConnectorPermissionInput, 0, targetCount*actionCount)
	for _, pair := range pairs {
		for index := range actionCount {
			inputs = append(inputs, ConnectorPermissionInput{
				TargetID: pair[0], ProfileID: pair[1], ActionName: fmt.Sprintf("inspect_%02d", index),
				ExecutionRule: string(connectortargets.ActionPermissionApprovalRequired),
			})
		}
	}
	path := "/tokens/" + strconv.FormatInt(fixture.tokenID, 10) + "/connector-permissions"
	response := performAccessRequest(t, fixture.mux, http.MethodPut, path, UpdateConnectorPermissionsRequest{
		ExpectedRevision: responseRevision(t, fixture.mux, path), Permissions: inputs,
	})
	assertChangedResponse(t, response, true)
	if got := countRows(t, fixture.database, "token_connector_action_permissions"); got != len(inputs) {
		t.Fatalf("persisted permissions = %d, want %d", got, len(inputs))
	}
	var payload string
	if err := fixture.database.QueryRow(`SELECT payload_json FROM audit_outbox WHERE action = 'token.connector_permissions.updated'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var audit map[string]any
	if err := json.Unmarshal([]byte(payload), &audit); err != nil {
		t.Fatal(err)
	}
	if audit["permission_count"] != float64(len(inputs)) || audit["previous_count"] != float64(0) || audit["revision"] == audit["previous_revision"] ||
		audit["permissions_truncated"] != false || len(audit["permissions"].([]any)) != len(inputs) {
		t.Fatalf("unexpected audit summary: %s", payload)
	}
}

func TestPermissionAuditRowsBoundLargeMetadata(t *testing.T) {
	permissions := make([]connectortargets.ActionPermission, 500)
	for index := range permissions {
		permissions[index] = connectortargets.ActionPermission{
			TargetID: int64(index + 1), ProfileID: 1, ActionName: "inspect_" + strings.Repeat("x", 200),
			ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
		}
	}
	rows, truncated := boundedPermissionAuditRows(permissions)
	if !truncated || len(rows) == len(permissions) {
		t.Fatalf("large permission metadata was not bounded: rows=%d truncated=%t", len(rows), truncated)
	}
	encoded, err := json.Marshal(rows)
	if err != nil || len(encoded) > 32*1024+2 {
		t.Fatalf("bounded audit rows: bytes=%d err=%v", len(encoded), err)
	}
}

func performAccessRequest(t *testing.T, handler http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func responseRevision(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	response := performAccessRequest(t, handler, http.MethodGet, path, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("load revision: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Revision == "" {
		t.Fatalf("decode revision: revision=%q err=%v", body.Revision, err)
	}
	return body.Revision
}

func assertChangedResponse(t *testing.T, response *httptest.ResponseRecorder, changed bool) {
	t.Helper()
	want := `"changed":` + strconv.FormatBool(changed)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), want) {
		t.Fatalf("update response: %d %s; want %s", response.Code, response.Body.String(), want)
	}
}
