package api

import (
	"context"
	"net"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

var errConnectorTransportApprovalChanged = connectorapi.ErrApprovalChanged

const maxConnectorCommandTimeout = connectorapi.MaxCommandTimeout

type approvedConnectorTransports = connectorapi.Approved

func newApprovedConnectorTransports(dependencies []actions.ResolvedDependency) approvedConnectorTransports {
	return connectorapi.NewApproved(dependencies)
}

type connectorNetworkTransport struct {
	server   *Server
	runtime  databaseRuntime
	approved approvedConnectorTransports
}

func (connectorNetworkTransport) ConnectorRuntimeCapability() string {
	return connectorapi.NetworkTransportCapabilityName
}

func (transport connectorNetworkTransport) DialConnectorTCP(ctx context.Context, request connectorapi.NetworkDialRequest) (net.Conn, error) {
	return connectorapi.Network{
		Dependencies: transport.dependencies(), Approved: transport.approved,
	}.DialConnectorTCP(ctx, request)
}

type connectorCommandTransport struct {
	server   *Server
	runtime  databaseRuntime
	approved approvedConnectorTransports
}

func (connectorCommandTransport) ConnectorRuntimeCapability() string {
	return connectorapi.CommandTransportCapabilityName
}

func (transport connectorCommandTransport) RunConnectorCommand(ctx context.Context, request connectorapi.CommandRunRequest) (connectorapi.CommandRunResult, error) {
	return connectorapi.Command{
		Dependencies: connectorTransportDependencies(transport.server, transport.runtime), Approved: transport.approved,
	}.RunConnectorCommand(ctx, request)
}

func (transport connectorNetworkTransport) dependencies() connectorapi.Dependencies {
	return connectorTransportDependencies(transport.server, transport.runtime)
}

func connectorTransportDependencies(server *Server, runtime databaseRuntime) connectorapi.Dependencies {
	var adapterFor connectorapi.AdapterProvider
	var trustStorePath func() string
	if server != nil {
		adapterFor = func(kind string) connectorapi.Adapter { return server.connectorAPIAdapterFor(kind) }
		trustStorePath = server.connectorTrustStorePath
	}
	return connectorapi.Dependencies{
		Runtime: connectorWorkspace(runtime).Connector, AdapterFor: adapterFor, TrustStorePath: trustStorePath,
	}
}
