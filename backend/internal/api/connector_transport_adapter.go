package api

import (
	"context"
	"net"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
)

var errConnectorTransportApprovalChanged = connectortransport.ErrApprovalChanged

const maxConnectorCommandTimeout = connectortransport.MaxCommandTimeout

type approvedConnectorTransports = connectortransport.Approved

func newApprovedConnectorTransports(dependencies []actions.ResolvedDependency) approvedConnectorTransports {
	return connectortransport.NewApproved(dependencies)
}

type connectorNetworkTransport struct {
	server   *Server
	runtime  *databaseRuntime
	approved approvedConnectorTransports
}

func (connectorNetworkTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (transport connectorNetworkTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	return connectortransport.Network{
		Dependencies: transport.dependencies(), Approved: transport.approved,
	}.DialConnectorTCP(ctx, request)
}

type connectorCommandTransport struct {
	server   *Server
	runtime  *databaseRuntime
	approved approvedConnectorTransports
}

func (connectorCommandTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (transport connectorCommandTransport) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	return connectortransport.Command{
		Dependencies: connectorTransportDependencies(transport.server, transport.runtime), Approved: transport.approved,
	}.RunConnectorCommand(ctx, request)
}

func (transport connectorNetworkTransport) dependencies() connectortransport.Dependencies {
	return connectorTransportDependencies(transport.server, transport.runtime)
}

func connectorTransportDependencies(server *Server, runtime *databaseRuntime) connectortransport.Dependencies {
	var adapterFor connectortransport.AdapterProvider
	var trustStorePath func() string
	if server != nil {
		adapterFor = func(kind string) connectorapi.Adapter { return server.connectorAPIAdapterFor(kind) }
		trustStorePath = server.connectorTrustStorePath
	}
	return connectortransport.Dependencies{
		Runtime: runtime, AdapterFor: adapterFor, TrustStorePath: trustStorePath,
	}
}
