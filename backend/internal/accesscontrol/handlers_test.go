package accesscontrol

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const (
	testConnectorKind = "access_test"
	testActionName    = "inspect"
)

type accessTestConnector struct{}

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
func (accessTestConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{{
		Name: testActionName, Label: "Inspect", Description: "Inspect test state.", Risk: connectors.RiskRead,
	}}, nil
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
	if err := registry.Register(accessTestConnector{}); err != nil {
		t.Fatal(err)
	}
	fixture := &handlerFixture{
		database: database, tokenID: token.ID, projectID: project.ID,
		targetID: target.ID, profileID: profile.ID, mux: http.NewServeMux(),
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Database: database, Tokens: tokens.NewStore(database), Registry: registry,
			ReusableTokens:   func(context.Context) (bool, error) { return false, nil },
			Mutate:           auditRunner(database, nil),
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
