package connectorports

import (
	"context"
	"net"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// Only the reviewed transport interface crosses into adapter code. Concrete
// implementation dependencies include core-only database and scope authority.
type networkDelegate struct{ transport connectors.NetworkTransport }

func (networkDelegate) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (delegate networkDelegate) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	return delegate.transport.DialConnectorTCP(ctx, request)
}

type commandDelegate struct{ transport connectors.CommandTransport }

func (commandDelegate) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (delegate commandDelegate) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	return delegate.transport.RunConnectorCommand(ctx, request)
}
