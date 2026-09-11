package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const targetDraftTestKind = "draft_test"

type targetDraftTestConnector struct{}

func (targetDraftTestConnector) Kind() string    { return targetDraftTestKind }
func (targetDraftTestConnector) Label() string   { return "Draft test" }
func (targetDraftTestConnector) Version() string { return "0.1" }
func (targetDraftTestConnector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{{Name: "endpoint", Type: connectors.FieldString, Default: "default-endpoint"}}}
}
func (targetDraftTestConnector) CredentialSchemas() []connectors.CredentialSchema { return nil }
func (targetDraftTestConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{Connector: targetDraftTestKind}, nil
}
func (targetDraftTestConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return nil, nil
}
func (targetDraftTestConnector) PrepareAction(context.Context, connectors.ActionRequest) (connectors.PreparedAction, error) {
	return connectors.PreparedAction{}, nil
}
func (targetDraftTestConnector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	return connectors.ActionResult{}, nil
}

type targetDraftTestAdapter struct {
	request connectormanagement.CreateTargetRequest
}

func (adapter *targetDraftTestAdapter) TestDraft(_ connectorapi.PeerIdentityGateway, w http.ResponseWriter, _ *http.Request, _ connectorapi.ConnectorDataRuntime, request any) {
	adapter.request = request.(connectormanagement.CreateTargetRequest)
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type targetDraftPeer struct{}

func (targetDraftPeer) ConnectorTrustStorePath() string { return "fixture-known-hosts" }

type targetDraftRuntime struct{}

func (targetDraftRuntime) ResolveConnectorActionTarget(context.Context, string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	return connectors.TargetView{}, connectors.CredentialProfileView{}, nil
}
func (targetDraftRuntime) EnsureRuntimeSurface(context.Context, connectortargets.EnsureRuntimeSurfaceInput) (connectortargets.RuntimeSurface, error) {
	return connectortargets.RuntimeSurface{}, nil
}
func (targetDraftRuntime) ListRuntimeSurfacesForProfile(context.Context, int64, int64, string) ([]connectortargets.RuntimeSurface, error) {
	return nil, nil
}
func (targetDraftRuntime) TargetProfileByRuntimeID(context.Context, int64) (connectors.TargetView, connectors.CredentialProfileView, connectortargets.RuntimeSurface, error) {
	return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.RuntimeSurface{}, nil
}
func (targetDraftRuntime) ListCredentialProfiles(context.Context, int64) ([]connectors.CredentialProfileView, error) {
	return nil, nil
}
func (targetDraftRuntime) CredentialResources(string) connectorapi.CredentialResourceStore {
	return nil
}

func TestTargetDraftHandlerNormalizesAndDispatches(t *testing.T) {
	database, registry := targetDraftFixture(t)
	adapter := &targetDraftTestAdapter{}
	adapters := connectorapi.NewRegistry()
	if err := adapters.Register(targetDraftTestKind, adapter); err != nil {
		t.Fatal(err)
	}
	component := New(Dependencies{
		Active: func(http.ResponseWriter) (Workspace, bool) {
			return Workspace{
				Storage:  StoragePorts{Database: database, Registry: registry},
				Adapters: TargetAdapterPorts{DataRuntime: func(string) connectorapi.ConnectorDataRuntime { return targetDraftRuntime{} }},
			}, true
		},
		Adapters: adapters, PeerIdentity: targetDraftPeer{},
		Capabilities: CapabilityDependencies{HasTCPTransport: func(string) bool { return false }},
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/connector-targets/test", strings.NewReader(`{"connector_kind":" draft_test ","name":"sample"}`))
	request.Header.Set("Content-Type", "application/json")
	component.HTTPHandlers().TargetDraft.Test(response, request)
	if response.Code != http.StatusOK || adapter.request.ConnectorKind != targetDraftTestKind || adapter.request.Config["endpoint"] != "default-endpoint" {
		t.Fatalf("response=%d %s request=%#v", response.Code, response.Body.String(), adapter.request)
	}
}

func TestTargetDraftHandlerRejectsUnsupportedAndIncompletePorts(t *testing.T) {
	database, registry := targetDraftFixture(t)
	for _, testCase := range []struct {
		name      string
		workspace Workspace
		body      string
		status    int
	}{
		{name: "unsupported", workspace: Workspace{Storage: StoragePorts{Database: database, Registry: registry}}, body: `{"connector_kind":"missing"}`, status: http.StatusBadRequest},
		{name: "missing runtime", workspace: Workspace{Storage: StoragePorts{Database: database, Registry: registry}}, body: `{"connector_kind":"draft_test"}`, status: http.StatusBadRequest},
		{name: "missing storage", workspace: Workspace{}, body: `{"connector_kind":"draft_test"}`, status: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			component := New(Dependencies{
				Active:   func(http.ResponseWriter) (Workspace, bool) { return testCase.workspace, true },
				Adapters: connectorapi.NewRegistry(), PeerIdentity: targetDraftPeer{},
				Capabilities: CapabilityDependencies{HasTCPTransport: func(string) bool { return false }},
			})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/connector-targets/test", strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			component.HTTPHandlers().TargetDraft.Test(response, request)
			if response.Code != testCase.status {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestTargetDraftHandlerFailsClosedWithoutDataRuntime(t *testing.T) {
	database, registry := targetDraftFixture(t)
	adapters := connectorapi.NewRegistry()
	if err := adapters.Register(targetDraftTestKind, &targetDraftTestAdapter{}); err != nil {
		t.Fatal(err)
	}
	component := New(Dependencies{
		Active: func(http.ResponseWriter) (Workspace, bool) {
			return Workspace{Storage: StoragePorts{Database: database, Registry: registry}}, true
		},
		Adapters: adapters, PeerIdentity: targetDraftPeer{},
		Capabilities: CapabilityDependencies{HasTCPTransport: func(string) bool { return false }},
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/connector-targets/test", strings.NewReader(`{"connector_kind":"draft_test"}`))
	request.Header.Set("Content-Type", "application/json")
	component.HTTPHandlers().TargetDraft.Test(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}

func targetDraftFixture(t *testing.T) (*sql.DB, *connectors.Registry) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "draft.aipdb"), "DraftPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	registry := connectors.NewRegistry()
	if err := registry.Register(targetDraftTestConnector{}); err != nil {
		t.Fatal(err)
	}
	return database, registry
}
