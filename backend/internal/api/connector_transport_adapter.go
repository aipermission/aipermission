package api

import (
	"context"
	"net"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
)

var errConnectorTransportApprovalChanged = connectors.ErrApprovalChanged

const maxConnectorCommandTimeout = connectors.MaxCommandTimeout

type approvedConnectorTransports = connectors.Approved

func newApprovedConnectorTransports(dependencies []actions.ResolvedDependency) approvedConnectorTransports {
	return connectors.NewApproved(dependencies)
}

type connectorNetworkTransport struct {
	server   *Server
	runtime  databaseRuntime
	approved approvedConnectorTransports
}

func (connectorNetworkTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (transport connectorNetworkTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	return connectors.Network{
		Dependencies: transport.dependencies(), Approved: transport.approved,
	}.DialConnectorTCP(ctx, request)
}

type connectorCommandTransport struct {
	server   *Server
	runtime  databaseRuntime
	approved approvedConnectorTransports
}

func (connectorCommandTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (transport connectorCommandTransport) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	return connectors.Command{
		Dependencies: connectorTransportDependencies(transport.server, transport.runtime), Approved: transport.approved,
	}.RunConnectorCommand(ctx, request)
}

func (transport connectorNetworkTransport) dependencies() connectors.Dependencies {
	return connectorTransportDependencies(transport.server, transport.runtime)
}

func connectorTransportDependencies(server *Server, runtime databaseRuntime) connectors.Dependencies {
	var adapterFor connectors.AdapterProvider
	var trustStorePath func() string
	if server != nil {
		adapterFor = func(kind string) connectorapi.Adapter { return server.connectorAPIAdapterFor(kind) }
		trustStorePath = server.connectorTrustStorePath
	}
	return connectors.Dependencies{
		Runtime: connectorWorkspace(runtime).Connector, AdapterFor: adapterFor, TrustStorePath: trustStorePath,
	}
}
