package connectorports

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func transportDependencies(workspace Workspace, adapterFor func(string) connectorapi.Adapter, trustStorePath func() string) connectortransport.Dependencies {
	return connectortransport.Dependencies{
		Runtime: workspace.runtime, AdapterFor: adapterFor, TrustStorePath: trustStorePath,
	}
}

func NetworkTransport(workspace Workspace, adapterFor func(string) connectorapi.Adapter, trustStorePath func() string) connectors.NetworkTransport {
	return connectortransport.Network{Dependencies: transportDependencies(workspace, adapterFor, trustStorePath)}
}

func CommandTransport(workspace Workspace, adapterFor func(string) connectorapi.Adapter, trustStorePath func() string) connectors.CommandTransport {
	return connectortransport.Command{Dependencies: transportDependencies(workspace, adapterFor, trustStorePath)}
}

func ApprovedNetworkTransport(workspace Workspace, adapterFor func(string) connectorapi.Adapter, trustStorePath func() string, dependencies []connectors.ResolvedDependency) connectors.NetworkTransport {
	return connectortransport.Network{
		Dependencies: transportDependencies(workspace, adapterFor, trustStorePath),
		Approved:     connectortransport.NewApproved(dependencies),
	}
}

func ApprovedCommandTransport(workspace Workspace, adapterFor func(string) connectorapi.Adapter, trustStorePath func() string, dependencies []connectors.ResolvedDependency) connectors.CommandTransport {
	return connectortransport.Command{
		Dependencies: transportDependencies(workspace, adapterFor, trustStorePath),
		Approved:     connectortransport.NewApproved(dependencies),
	}
}
