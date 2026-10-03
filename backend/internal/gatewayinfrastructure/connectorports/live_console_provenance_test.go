package connectorports

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type launchFixture struct {
	request connectorapi.LiveConsoleOpenRequest
	called  bool
}

func (*launchFixture) ConnectorScope(string, connectorruntime.SecretAccessorFactory) *connectorruntime.Scope {
	return connectorruntime.NewScope("fixture", connectorruntime.Dependencies{})
}
func (*launchFixture) LiveConsoleCapabilityKind() string {
	return connectorapi.RuntimeCapabilityLiveConsole
}
func (*launchFixture) LiveConsoleTargetRef(context.Context, connectorapi.LiveConsoleRuntime, int64) (string, error) {
	return "", nil
}
func (*launchFixture) LiveConsoleTargetMetadata(connectors.TargetView, connectors.CredentialProfileView) map[string]any {
	return nil
}
func (fixture *launchFixture) OpenLiveConsole(_ context.Context, _ connectorapi.LiveConsoleGateway, _ connectorapi.LiveConsoleRuntime, request connectorapi.LiveConsoleOpenRequest) (*connectorapi.LiveConsoleSession, error) {
	fixture.request, fixture.called = request, true
	return &connectorapi.LiveConsoleSession{}, nil
}

func TestNestedConsoleGatewayMarksOnlyResolvedConnectorDelegation(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "console-launch.db"), "console-launch-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "Console launch fixture", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "fixture", Kind: "fixture", Label: "Default", Public: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &launchFixture{}
	component := NewPorts(PortsDependencies{LiveConsole: LiveConsoleDependencies{
		TransportAdapter: func(string) connectorapi.LiveConsoleTransportAdapter { return fixture },
		TargetAdapter:    func(string) connectorapi.LiveConsoleTargetAdapter { return fixture },
	}})
	gateway := component.LiveConsoleGateway(NewWorkspace(fixture, database, nil, nil), "fixture:10:11")
	params := map[string]any{"launch_parameter": "connector-owned", "NestedTransport": false}
	ref := connectors.FormatTargetRef("fixture", target.ID, profile.ID)
	if _, err := gateway.ConnectorOpenLiveConsole(t.Context(), ref, 24, 80, params); err != nil {
		t.Fatal(err)
	}
	request := fixture.request
	if !fixture.called || !request.NestedTransport || request.RuntimeID < 1 || request.Rows != 24 || request.Cols != 80 || !reflect.DeepEqual(request.Params, params) {
		t.Fatalf("resolved nested launch lost provenance: %#v", request)
	}
	fixture.called = false
	if _, err := gateway.ConnectorOpenLiveConsole(t.Context(), "invalid", 24, 80, params); err == nil || fixture.called {
		t.Fatalf("invalid nested target reached transport: called=%v err=%v", fixture.called, err)
	}
}
