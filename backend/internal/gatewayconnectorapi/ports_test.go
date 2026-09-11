package gatewayconnectorapi

import "testing"

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
