package api

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestConnectorRuntimeActionGatewayRejectsCrossConnectorRuntime(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "alpha",
		Name:          "alpha target",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: target.ConnectorKind,
		Kind:          "default",
		Label:         "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := store.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind:  target.ConnectorKind,
		TargetID:       target.ID,
		ProfileID:      profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
	})
	if err != nil {
		t.Fatal(err)
	}
	port, _ := fixture.server.connectorRuntime.RuntimeActionPorts(runtime, "beta")
	_, err = port.ConnectorRestartConsoleSession(context.Background(), connectorapi.Principal{}, surface.ID, "test")
	if !errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		t.Fatalf("cross-connector restart error = %v", err)
	}
}
