package connectorports

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestConcreteConnectorPortsExposeOnlyTheirDeclaredAuthority(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		methods []string
	}{
		{name: "peer gateway", value: PeerGateway{}, methods: []string{"ConnectorTrustStorePath"}},
		{name: "live console gateway", value: LiveConsoleGateway{}, methods: []string{"ConnectorOpenLiveConsole", "ConnectorRunCommand", "ConnectorTrustStorePath"}},
		{name: "route gateway", value: RouteGateway{}, methods: []string{"ConnectorActiveRuntimeAvailable", "ConnectorChangeVaultPeerTrust", "ConnectorTrustStorePath"}},
		{name: "runtime action gateway", value: RuntimeActionGateway{}, methods: []string{"ConnectorCreateAndRunDownloadBatch", "ConnectorRestartConsoleSession", "ConnectorTrustStorePath"}},
		{name: "action finish gateway", value: ActionFinishGateway{}, methods: []string{"ConnectorFinishActionRequest"}},
		{name: "file transfer gateway", value: FileTransferGateway{}, methods: []string{"ConnectorRuntimeCapabilities", "ConnectorTrustStorePath"}},
		{name: "target deletion gateway", value: TargetDeletionGateway{}, methods: []string{"ConnectorDeleteTargetRecord", "ConnectorFinalizeDeletedTarget", "ConnectorRestartConsoleSession", "ConnectorTrustStorePath"}},
		{name: "target operation gateway", value: TargetOperationGateway{}, methods: []string{"ConnectorTrustStorePath", "ConnectorWriteAudit", "ConnectorWriteTargetAudit"}},
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

func TestConnectorTargetDeletionGatewayRejectsUnboundTarget(t *testing.T) {
	port := NewPorts(PortsDependencies{}).TargetDeletionGateway(Workspace{}, "alpha", 41)
	err := port.ConnectorDeleteTargetRecord(context.Background(), connectorapi.Target{ID: 42, ConnectorKind: "alpha"}, nil)
	if !errors.Is(err, connectortargets.ErrTargetNotFound) {
		t.Fatalf("unbound target deletion error = %v", err)
	}
}
