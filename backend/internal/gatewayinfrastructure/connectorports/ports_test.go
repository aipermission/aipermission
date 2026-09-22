package connectorports

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
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

func TestAcquireDeliveryAdmissionOwnsOneNestedDeliveryWindow(t *testing.T) {
	identity := &connectors.DeliveryAdmissionIdentity{}
	acquires := 0
	releases := 0
	workspace := NewWorkspace(nil, nil, func(context.Context) (func(), error) {
		acquires++
		return func() { releases++ }, nil
	}, identity)

	ctx, release, err := AcquireDeliveryAdmission(t.Context(), workspace)
	if err != nil || !connectors.DeliveryAdmissionHeld(ctx, identity) || acquires != 1 {
		t.Fatalf("outer admission ctx=%v acquires=%d err=%v", connectors.DeliveryAdmissionHeld(ctx, identity), acquires, err)
	}
	_, nestedRelease, err := AcquireDeliveryAdmission(ctx, workspace)
	if err != nil || acquires != 1 {
		t.Fatalf("nested admission acquired again: acquires=%d err=%v", acquires, err)
	}
	nestedRelease()
	if releases != 0 {
		t.Fatalf("nested release closed the outer admission: releases=%d", releases)
	}
	release()
	if releases != 1 {
		t.Fatalf("outer release count=%d", releases)
	}
}

func TestIncompletePortsDependenciesFailClosed(t *testing.T) {
	component := NewPorts(PortsDependencies{})
	if component.RouteGateway().ConnectorActiveRuntimeAvailable(httptest.NewRecorder()) {
		t.Fatal("route gateway accepted a missing active-runtime dependency")
	}
	if _, err := component.LiveConsoleGateway(Workspace{}, "").ConnectorOpenLiveConsole(t.Context(), "test", 24, 80, nil); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("live console error = %v, want %v", err, ErrRuntimeUnavailable)
	}
}
