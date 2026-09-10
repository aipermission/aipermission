package connectormanagement

import (
	"context"
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
	return []connectors.CredentialSchema{{Kind: "operator", Label: "Operator"}}
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

type managementHTTPFixture struct {
	mux             *http.ServeMux
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
		mux: mux, target: target, profile: profile,
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
