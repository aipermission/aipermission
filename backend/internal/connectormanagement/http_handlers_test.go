package connectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
)

const managementTestConnectorKind = "management_test"

type managementTestConnector struct{}

func (managementTestConnector) Kind() string    { return managementTestConnectorKind }
func (managementTestConnector) Label() string   { return "Management test" }
func (managementTestConnector) Version() string { return "0.1" }
func (managementTestConnector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{{Name: "endpoint", Type: connectors.FieldString}}}
}
func (managementTestConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{
		Kind: "operator", Label: "Operator",
		Schema: connectors.Schema{Fields: []connectors.Field{
			{Name: "managed_marker", Type: connectors.FieldString},
			{Name: "password", Type: connectors.FieldSecret, Secret: true},
		}},
	}}
}
func (managementTestConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{Connector: managementTestConnectorKind, ConnectorID: managementTestConnectorKind}, nil
}
func (managementTestConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{{
		Name: "inspect", Label: "Inspect", Description: "Inspect target state.", Risk: connectors.RiskRead,
	}}, nil
}
func (managementTestConnector) PrepareAction(context.Context, connectors.ActionRequest) (connectors.PreparedAction, error) {
	return connectors.PreparedAction{ConnectorKind: managementTestConnectorKind, ActionName: "inspect"}, nil
}
func (managementTestConnector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
}
func (managementTestConnector) TestConnection(context.Context, connectors.RuntimeContext) (connectors.TestResult, error) {
	return connectors.TestResult{
		Status: connectors.TestOK, Message: "connection ready",
		Details: map[string]any{"transport": "fixture"},
	}, nil
}
func (managementTestConnector) ProvisionCredentialProfile(context.Context, connectors.RuntimeContext, map[string]any) (connectors.ProvisionedCredentialProfile, error) {
	return connectors.ProvisionedCredentialProfile{
		Kind: "operator", Label: "managed", Public: map[string]any{"managed_marker": "generated"},
		Secret: map[string]any{"password": "managed-secret"}, RiskLabel: "write",
		Result: connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"created": true}},
	}, nil
}
func (managementTestConnector) CleanupProvisionedCredentialProfile(context.Context, connectors.RuntimeContext, connectors.CredentialProfileView) (connectors.ActionResult, error) {
	return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
}
func (managementTestConnector) PreserveProvisionedCredentialPublic(existing connectors.CredentialProfileView, requested map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(requested)+1)
	for key, value := range requested {
		result[key] = value
	}
	if marker, ok := existing.Public["managed_marker"]; ok {
		result["managed_marker"] = marker
	}
	return result, nil
}
func (managementTestConnector) ProvisionedCredentialAdminProfileID(connectors.CredentialProfileView) (int64, bool, error) {
	return 0, false, nil
}

type managementHTTPFixture struct {
	mux             *http.ServeMux
	database        *sql.DB
	registry        *connectors.Registry
	target          connectortargets.Target
	profile         connectortargets.CredentialProfile
	liveSurface     connectortargets.RuntimeSurface
	transferSurface connectortargets.RuntimeSurface
}

func newManagementHTTPFixture(t *testing.T) *managementHTTPFixture {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "management.aipdb"), "ManagementPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	registry := connectors.NewRegistry()
	if err := registry.Register(managementTestConnector{}); err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: managementTestConnectorKind,
		Name:          "Primary target",
		Config:        map[string]any{"endpoint": "local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: managementTestConnectorKind,
		Kind: "operator", Label: "main", Public: map[string]any{"username": "operator"},
	})
	if err != nil {
		t.Fatal(err)
	}
	liveSurface, err := store.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: managementTestConnectorKind, TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: "test_console", Label: "Test console",
	})
	if err != nil {
		t.Fatal(err)
	}
	transferSurface, err := store.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: managementTestConnectorKind, TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityFileTransfer, Label: "Files",
	})
	if err != nil {
		t.Fatal(err)
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Database: database,
			Registry: registry,
			Features: func(kind string) ConnectorFeatures {
				if kind != managementTestConnectorKind {
					return ConnectorFeatures{}
				}
				return ConnectorFeatures{LiveConsoleCapability: "test_console", FileTransfer: true}
			},
			SessionEnvironmentSupported: func(_ context.Context, runtimeID int64) bool {
				return runtimeID == liveSurface.ID
			},
		}, true
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /connectors", handlers.ListConnectors)
	mux.HandleFunc("GET /connectors/{kind}", handlers.GetConnector)
	mux.HandleFunc("GET /targets", handlers.ListTargetProfiles)
	mux.HandleFunc("GET /connector-targets", handlers.ListTargets)
	mux.HandleFunc("GET /connector-targets/inventory", handlers.ListTargetInventory)
	mux.HandleFunc("GET /connector-targets/{id}", handlers.GetTarget)
	mux.HandleFunc("GET /connector-targets/{id}/profiles", handlers.ListCredentialProfiles)
	mux.HandleFunc("GET /connector-targets/{id}/profiles/{profile_id}/actions", handlers.ListCredentialProfileActions)
	return &managementHTTPFixture{
		mux: mux, database: database, registry: registry, target: target, profile: profile,
		liveSurface: liveSurface, transferSurface: transferSurface,
	}
}

func TestConnectorCatalogHandlersOwnListAndDetailContracts(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	list := performManagementRequest(fixture.mux, "/connectors")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"kind":"management_test"`) {
		t.Fatalf("list catalog: %d %s", list.Code, list.Body.String())
	}
	detail := performManagementRequest(fixture.mux, "/connectors/management_test")
	for _, expected := range []string{`"label":"Management test"`, `"target_schema"`, `"credential_schemas"`, `"help"`} {
		if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), expected) {
			t.Fatalf("connector detail missing %s: %d %s", expected, detail.Code, detail.Body.String())
		}
	}
	if response := performManagementRequest(fixture.mux, "/connectors/Bad-Kind"); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind status = %d", response.Code)
	}
	if response := performManagementRequest(fixture.mux, "/connectors/missing"); response.Code != http.StatusNotFound {
		t.Fatalf("missing kind status = %d", response.Code)
	}
}

func TestTargetQueryHandlersOwnProfilesInventoryAndLookup(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	profilesResponse := performManagementRequest(fixture.mux, "/targets")
	var profilesPage struct {
		Items []TargetProfileItem `json:"items"`
	}
	decodeManagementResponse(t, profilesResponse, &profilesPage)
	if profilesResponse.Code != http.StatusOK || len(profilesPage.Items) != 1 {
		t.Fatalf("target profiles: %d %s", profilesResponse.Code, profilesResponse.Body.String())
	}
	profileItem := profilesPage.Items[0]
	if profileItem.Ref != connectors.FormatTargetRef(managementTestConnectorKind, fixture.target.ID, fixture.profile.ID) ||
		profileItem.RuntimeID != fixture.liveSurface.ID || profileItem.TransferRuntimeID != fixture.transferSurface.ID ||
		profileItem.Config["endpoint"] != "local" || profileItem.Public["username"] != "operator" {
		t.Fatalf("target profile item = %#v", profileItem)
	}

	filtered := performManagementRequest(fixture.mux, "/connector-targets?kind=missing")
	var filteredPage struct {
		Items []TargetResponse `json:"items"`
	}
	decodeManagementResponse(t, filtered, &filteredPage)
	if filtered.Code != http.StatusOK || len(filteredPage.Items) != 0 {
		t.Fatalf("filtered targets: %d %s", filtered.Code, filtered.Body.String())
	}

	inventory := performManagementRequest(fixture.mux, "/connector-targets/inventory")
	var inventoryPage struct {
		Items []TargetResponse `json:"items"`
	}
	decodeManagementResponse(t, inventory, &inventoryPage)
	if inventory.Code != http.StatusOK || len(inventoryPage.Items) != 1 || len(inventoryPage.Items[0].Profiles) != 1 {
		t.Fatalf("inventory: %d %s", inventory.Code, inventory.Body.String())
	}
	summary := inventoryPage.Items[0].Profiles[0]
	if len(summary.Actions) != 1 || summary.Actions[0].Name != "inspect" || summary.RuntimeID != fixture.liveSurface.ID ||
		summary.TransferRuntimeID != fixture.transferSurface.ID || !summary.VaultSession {
		t.Fatalf("inventory summary = %#v", summary)
	}

	target := performManagementRequest(fixture.mux, "/connector-targets/"+strconv.FormatInt(fixture.target.ID, 10))
	var targetBody TargetResponse
	decodeManagementResponse(t, target, &targetBody)
	if target.Code != http.StatusOK || targetBody.ID != fixture.target.ID || len(targetBody.Profiles) != 1 {
		t.Fatalf("target detail: %d %s", target.Code, target.Body.String())
	}
	if response := performManagementRequest(fixture.mux, "/connector-targets/999999"); response.Code != http.StatusNotFound {
		t.Fatalf("missing target status = %d", response.Code)
	}

	profiles := performManagementRequest(fixture.mux, "/connector-targets/"+strconv.FormatInt(fixture.target.ID, 10)+"/profiles")
	var profilesBody struct {
		Items []ProfileSummary `json:"items"`
	}
	decodeManagementResponse(t, profiles, &profilesBody)
	if profiles.Code != http.StatusOK || len(profilesBody.Items) != 1 || profilesBody.Items[0].ID != fixture.profile.ID {
		t.Fatalf("credential profiles: %d %s", profiles.Code, profiles.Body.String())
	}

	actions := performManagementRequest(
		fixture.mux,
		"/connector-targets/"+strconv.FormatInt(fixture.target.ID, 10)+"/profiles/"+strconv.FormatInt(fixture.profile.ID, 10)+"/actions",
	)
	var actionsBody struct {
		Items []connectors.ActionDefinition `json:"items"`
	}
	decodeManagementResponse(t, actions, &actionsBody)
	if actions.Code != http.StatusOK || len(actionsBody.Items) != 1 || actionsBody.Items[0].Name != "inspect" {
		t.Fatalf("credential profile actions: %d %s", actions.Code, actions.Body.String())
	}
}

func TestConnectorManagementScopeFailsClosed(t *testing.T) {
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) { return Scope{}, true })
	response := httptest.NewRecorder()
	handlers.ListTargets(response, httptest.NewRequest(http.MethodGet, "/connector-targets", nil))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "configured") {
		t.Fatalf("missing scope response: %d %s", response.Code, response.Body.String())
	}
}

func TestTargetMutationHandlersOwnCreateAndUpdateTransactions(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	auditActions := []string{}
	acquired := 0
	released := 0
	exclusiveHeld := false
	ensuredProfiles := []int64{}
	lifecycleChanges := []TargetLifecycleChange{}
	handler := NewTargetMutationHTTPHandler(func(http.ResponseWriter) (TargetMutationScope, bool) {
		return TargetMutationScope{
			Database: fixture.database,
			Registry: fixture.registry,
			ValidateTransport: func(_ context.Context, projectID int64, config map[string]any) error {
				if !exclusiveHeld {
					t.Fatal("transport validation ran outside the exclusive lifecycle gate")
				}
				if projectID < 1 || config["endpoint"] == "" {
					return connectortargets.ValidationError("invalid transport fixture")
				}
				return nil
			},
			AcquireExclusive: func(context.Context) (func(), error) {
				acquired++
				exclusiveHeld = true
				return func() {
					exclusiveHeld = false
					released++
				}, nil
			},
			WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
				tx, err := fixture.database.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				appendAudit := func(_ *sql.Tx, _ string, _ *int64, _ int64, action string, _ any) error {
					auditActions = append(auditActions, action)
					return nil
				}
				if err := mutate(tx, appendAudit); err != nil {
					return err
				}
				return tx.Commit()
			},
			EnsureRuntimeSurfaces: func(_ context.Context, _ *connectortargets.Store, _ connectortargets.Target, profile connectortargets.CredentialProfile) error {
				ensuredProfiles = append(ensuredProfiles, profile.ID)
				return nil
			},
			AfterLifecycleChange: func(_ context.Context, change TargetLifecycleChange) error {
				lifecycleChanges = append(lifecycleChanges, change)
				return nil
			},
		}, true
	})

	create := performManagementJSON(t, handler.Create, "/connector-targets", CreateTargetRequest{
		ProjectID: fixture.target.ProjectID, ConnectorKind: managementTestConnectorKind,
		Name: "Secondary target", Config: map[string]any{"endpoint": "secondary"},
	})
	var created TargetResponse
	decodeManagementResponse(t, create, &created)
	if create.Code != http.StatusCreated || created.ID < 1 || created.Name != "Secondary target" {
		t.Fatalf("create target: %d %s", create.Code, create.Body.String())
	}

	updateRequest := UpdateTargetRequest{Name: "Primary renamed", Config: map[string]any{"endpoint": "updated"}}
	update := performManagementJSON(t, handler.Update, "/connector-targets/"+strconv.FormatInt(fixture.target.ID, 10), updateRequest)
	var updated TargetResponse
	decodeManagementResponse(t, update, &updated)
	if update.Code != http.StatusOK || updated.Name != "Primary renamed" || updated.ProjectID != fixture.target.ProjectID {
		t.Fatalf("update target: %d %s", update.Code, update.Body.String())
	}
	if acquired != 2 || released != 2 || exclusiveHeld || len(ensuredProfiles) != 1 || ensuredProfiles[0] != fixture.profile.ID {
		t.Fatalf("acquired=%d released=%d ensured=%v", acquired, released, ensuredProfiles)
	}
	if len(lifecycleChanges) != 1 || lifecycleChanges[0].TargetID != fixture.target.ID || lifecycleChanges[0].ProfileID != 0 {
		t.Fatalf("lifecycle changes = %#v", lifecycleChanges)
	}
	if strings.Join(auditActions, ",") != "connector.target.created,connector.target.updated" {
		t.Fatalf("audit actions = %v", auditActions)
	}
}

func TestTargetMutationHandlersFailClosedBeforeWriting(t *testing.T) {
	handler := NewTargetMutationHTTPHandler(func(http.ResponseWriter) (TargetMutationScope, bool) {
		return TargetMutationScope{}, true
	})
	response := performManagementJSON(t, handler.Create, "/connector-targets", CreateTargetRequest{
		ConnectorKind: managementTestConnectorKind, Name: "Never created",
	})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("incomplete scope status = %d: %s", response.Code, response.Body.String())
	}
}

func TestTargetCreateRequiresLifecycleAdmissionCapabilities(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	handler := NewTargetMutationHTTPHandler(func(http.ResponseWriter) (TargetMutationScope, bool) {
		return TargetMutationScope{
			Database:          fixture.database,
			Registry:          fixture.registry,
			ValidateTransport: func(context.Context, int64, map[string]any) error { return nil },
			AcquireExclusive: func(context.Context) (func(), error) {
				return func() {}, nil
			},
			WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
				tx, err := fixture.database.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				if err := mutate(tx, func(*sql.Tx, string, *int64, int64, string, any) error { return nil }); err != nil {
					return err
				}
				return tx.Commit()
			},
		}, true
	})
	response := performManagementJSON(t, handler.Create, "/connector-targets", CreateTargetRequest{
		ProjectID: fixture.target.ProjectID, ConnectorKind: managementTestConnectorKind,
		Name: "Create-only target", Config: map[string]any{"endpoint": "create-only"},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create-only scope status = %d: %s", response.Code, response.Body.String())
	}
}

func TestTargetCreateRejectsMissingAuditAppenderWithoutPersisting(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	handler := NewTargetMutationHTTPHandler(func(http.ResponseWriter) (TargetMutationScope, bool) {
		return TargetMutationScope{
			Database:          fixture.database,
			Registry:          fixture.registry,
			ValidateTransport: func(context.Context, int64, map[string]any) error { return nil },
			AcquireExclusive:  func(context.Context) (func(), error) { return func() {}, nil },
			WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
				tx, err := fixture.database.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				return mutate(tx, nil)
			},
		}, true
	})
	response := performManagementJSON(t, handler.Create, "/connector-targets", CreateTargetRequest{
		ProjectID: fixture.target.ProjectID, ConnectorKind: managementTestConnectorKind,
		Name: "Missing audit target", Config: map[string]any{"endpoint": "missing-audit"},
	})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing appender status = %d: %s", response.Code, response.Body.String())
	}
	targets, err := connectortargets.NewStore(fixture.database).ListTargets(t.Context(), connectortargets.ListTargetsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.Name == "Missing audit target" {
			t.Fatal("target persisted without an audit appender")
		}
	}
}

func performManagementRequest(handler http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func decodeManagementResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response: %v: %s", err, response.Body.String())
	}
}

func performManagementJSON(t *testing.T, handler http.HandlerFunc, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	if strings.Contains(path, "/connector-targets/") {
		request.SetPathValue("id", strings.TrimPrefix(path, "/connector-targets/"))
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}
