package filetransferhttp

import (
	"context"
	"testing"

	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

func TestConnectorPortsRejectUninitializedCredentialBoundary(t *testing.T) {
	fixture := newTransferTestFixture(t)
	execution := fixture.execution(t)
	fixture.resolver.resolve = func(context.Context, int64) (transferapp.ConnectorPorts, error) {
		return transferapp.ConnectorPorts{
			ConnectorKind: "fixture",
			Gateway:       testTransferGateway{},
			Runtime:       execution.runtime,
		}, nil
	}

	if _, err := connectorFileTransferPortsForID(t.Context(), fixture.runtime, fixture.runtimeID); err == nil {
		t.Fatal("uninitialized credential boundary was accepted")
	}
}
