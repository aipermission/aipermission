package connectorports

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func TestNilPortsComponentGatewayProvidersFailClosed(t *testing.T) {
	var component *PortsComponent
	workspace := Workspace{}

	if gateway := component.TargetDeletionGatewayProvider(workspace)("test", 1); gateway != nil {
		t.Fatalf("deletion gateway = %#v, want nil", gateway)
	}
	if gateway := component.TargetOperationGatewayProvider(workspace)("test", 1); gateway != nil {
		t.Fatalf("operation gateway = %#v, want nil", gateway)
	}
}

func TestIncompletePortsDependenciesFailClosed(t *testing.T) {
	component := NewPorts(PortsDependencies{})
	if component.RouteGateway().ConnectorActiveRuntimeAvailable(httptest.NewRecorder()) {
		t.Fatal("route gateway accepted a missing active-runtime dependency")
	}
	if _, err := component.LiveConsoleGateway(Workspace{}).ConnectorOpenLiveConsole(t.Context(), "test", 24, 80, nil); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("live console error = %v, want %v", err, ErrRuntimeUnavailable)
	}
}
