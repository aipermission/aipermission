package api

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func TestConcreteConnectorPortsExposeOnlyTheirDeclaredAuthority(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		methods []string
	}{
		{name: "lifecycle runtime", value: connectorTargetLifecycleRuntimePort{}, methods: []string{"ConnectorConsoleSessions", "ConnectorLocalExecutionPrincipal", "CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "peer gateway", value: connectorPeerGatewayPort{}, methods: []string{"ConnectorTrustStorePath"}},
		{name: "live console gateway", value: connectorLiveConsoleGatewayPort{}, methods: []string{"ConnectorOpenLiveConsole", "ConnectorTrustStorePath"}},
		{name: "route gateway", value: connectorRouteGatewayPort{}, methods: []string{"ConnectorActiveRuntimeAvailable", "ConnectorChangeVaultPeerTrust", "ConnectorTrustStorePath"}},
		{name: "runtime action gateway", value: connectorRuntimeActionGatewayPort{}, methods: []string{"ConnectorCreateDownloadBatch", "ConnectorRestartConsoleSession", "ConnectorRunTransferBatch", "ConnectorTrustStorePath"}},
		{name: "action finish gateway", value: connectorActionFinishGatewayPort{}, methods: []string{"ConnectorFinishActionRequest"}},
		{name: "file transfer gateway", value: connectorFileTransferGatewayPort{}, methods: []string{"ConnectorRuntimeCapabilities", "ConnectorTrustStorePath"}},
		{name: "target deletion gateway", value: connectorTargetDeletionGatewayPort{}, methods: []string{"ConnectorDeleteTargetRecord", "ConnectorFinalizeDeletedTarget", "ConnectorRestartConsoleSession", "ConnectorTrustStorePath"}},
		{name: "target operation gateway", value: connectorTargetOperationGatewayPort{}, methods: []string{"ConnectorTrustStorePath", "ConnectorWriteAudit"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typeValue := reflect.TypeOf(test.value)
			methods := make([]string, 0, typeValue.NumMethod())
			for index := 0; index < typeValue.NumMethod(); index++ {
				methods = append(methods, typeValue.Method(index).Name)
			}
			slices.Sort(methods)
			slices.Sort(test.methods)
			if !slices.Equal(methods, test.methods) {
				t.Fatalf("concrete methods = %v, want %v", methods, test.methods)
			}
		})
	}
}

func TestConnectorRuntimeActionGatewayRejectsCrossConnectorRuntime(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	store := connectortargets.NewStore(runtime.database)
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
	port := connectorRuntimeActionGatewayPort{
		connectorPeerGatewayPort: connectorPeerGatewayPort{server: fixture.server},
		runtime:                  runtime,
		kind:                     "beta",
	}
	_, err = port.ConnectorRestartConsoleSession(context.Background(), executionprincipal.Principal{}, surface.ID, "test")
	if !errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		t.Fatalf("cross-connector restart error = %v", err)
	}
}

func TestConnectorTargetDeletionGatewayRejectsUnboundTarget(t *testing.T) {
	port := connectorTargetDeletionGatewayPort{kind: "alpha", targetID: 41}
	err := port.ConnectorDeleteTargetRecord(context.Background(), connectortargets.Target{ID: 42, ConnectorKind: "alpha"}, nil)
	if !errors.Is(err, connectortargets.ErrTargetNotFound) {
		t.Fatalf("unbound target deletion error = %v", err)
	}
}
