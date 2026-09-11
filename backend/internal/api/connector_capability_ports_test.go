package api

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

func TestConcreteConnectorPortsExposeOnlyTheirDeclaredAuthority(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		methods []string
	}{
		{name: "peer gateway", value: connectorports.PeerGateway{}, methods: []string{"ConnectorTrustStorePath"}},
		{name: "live console gateway", value: connectorports.LiveConsoleGateway{}, methods: []string{"ConnectorOpenLiveConsole", "ConnectorTrustStorePath"}},
		{name: "route gateway", value: connectorports.RouteGateway{}, methods: []string{"ConnectorActiveRuntimeAvailable", "ConnectorChangeVaultPeerTrust", "ConnectorTrustStorePath"}},
		{name: "runtime action gateway", value: connectorports.RuntimeActionGateway{}, methods: []string{"ConnectorCreateAndRunDownloadBatch", "ConnectorRestartConsoleSession", "ConnectorTrustStorePath"}},
		{name: "action finish gateway", value: connectorports.ActionFinishGateway{}, methods: []string{"ConnectorFinishActionRequest"}},
		{name: "file transfer gateway", value: connectorports.FileTransferGateway{}, methods: []string{"ConnectorRuntimeCapabilities", "ConnectorTrustStorePath"}},
		{name: "target deletion gateway", value: connectorports.TargetDeletionGateway{}, methods: []string{"ConnectorDeleteTargetRecord", "ConnectorFinalizeDeletedTarget", "ConnectorRestartConsoleSession", "ConnectorTrustStorePath"}},
		{name: "target operation gateway", value: connectorports.TargetOperationGateway{}, methods: []string{"ConnectorTrustStorePath", "ConnectorWriteAudit"}},
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
	store := connectortargets.NewStore(runtime.StoragePort().DatabaseHandle())
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
	port, _ := fixture.server.connectorPortsApplication().RuntimeActionPorts(fixture.server.connectorPortsWorkspace(runtime), "beta")
	_, err = port.ConnectorRestartConsoleSession(context.Background(), executionprincipal.Principal{}, surface.ID, "test")
	if !errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		t.Fatalf("cross-connector restart error = %v", err)
	}
}

func TestConnectorTargetDeletionGatewayRejectsUnboundTarget(t *testing.T) {
	port := connectorports.NewPorts(connectorports.PortsDependencies{}).TargetDeletionGateway(connectorports.Workspace{}, "alpha", 41)
	err := port.ConnectorDeleteTargetRecord(context.Background(), connectortargets.Target{ID: 42, ConnectorKind: "alpha"}, nil)
	if !errors.Is(err, connectortargets.ErrTargetNotFound) {
		t.Fatalf("unbound target deletion error = %v", err)
	}
}
