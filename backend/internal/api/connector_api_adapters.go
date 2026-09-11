package api

import (
	"net/http"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (s *Server) connectorAPIAdapterFor(kind string) connectorapi.Adapter {
	return s.connectorAdapterRegistry().For(kind)
}

func connectorRuntimeCapabilitiesForAction(kind string, server *Server, runtime databaseRuntime, dependencies []actions.ResolvedDependency) connectorapi.RuntimeCapabilityResolver {
	resolver := connectorRuntimeCapabilitiesFor(kind, server, runtime)
	capabilities, _ := resolver.(connectorRuntimeCapabilities)
	if capabilities == nil {
		capabilities = connectorRuntimeCapabilities{}
	}
	workspace := server.connectorWorkspace(runtime)
	capabilities[connectorapi.NetworkTransportCapabilityName] = connectorapi.ApprovedNetworkTransport(workspace, server.connectorAPIAdapterFor, server.connectorTrustStorePath, dependencies)
	capabilities[connectorapi.CommandTransportCapabilityName] = connectorapi.ApprovedCommandTransport(workspace, server.connectorAPIAdapterFor, server.connectorTrustStorePath, dependencies)
	return capabilities
}

func runtimeConnectorAPIAdapterFor(runtime databaseRuntime, kind string) connectorapi.Adapter {
	return runtimeConnectorAdapterRegistry(runtime).For(kind)
}

func (s *Server) connectorRuntimeAdapterFor(kind string) connectorapi.RuntimeAdapter {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.RuntimeAdapter)
	return adapter
}

type connectorRuntimeCapabilities map[string]connectorapi.RuntimeCapability

func (c connectorRuntimeCapabilities) RuntimeCapability(name string) connectorapi.RuntimeCapability {
	return c[name]
}

func connectorRuntimeCapabilitiesFor(kind string, server *Server, runtime databaseRuntime) connectorapi.RuntimeCapabilityResolver {
	capabilities := connectorRuntimeCapabilities{}
	if server != nil && runtime != nil {
		workspace := server.connectorWorkspace(runtime)
		networkTransport := connectorapi.NetworkTransport(workspace, server.connectorAPIAdapterFor, server.connectorTrustStorePath)
		capabilities[networkTransport.ConnectorRuntimeCapability()] = networkTransport
		commandTransport := connectorapi.CommandTransport(workspace, server.connectorAPIAdapterFor, server.connectorTrustStorePath)
		capabilities[commandTransport.ConnectorRuntimeCapability()] = commandTransport
	}
	if server != nil {
		adapter := server.connectorRuntimeAdapterFor(kind)
		if adapter != nil {
			gatewayPort, runtimePort := server.connectorPortsApplication().RuntimeActionPorts(server.connectorPortsWorkspace(runtime), kind)
			for name, capability := range adapter.RuntimeCapabilities(gatewayPort, runtimePort) {
				if name == "" || capability == nil {
					continue
				}
				capabilities[name] = capability
			}
		}
	}
	if len(capabilities) == 0 {
		return nil
	}
	return capabilities
}

func registerConnectorAdapterRoutes(mux *http.ServeMux, server *Server) {
	connectorInfos := server.connectorRegistry().List()
	kinds := make([]string, 0, len(connectorInfos))
	for _, info := range connectorInfos {
		kinds = append(kinds, info.Kind)
	}
	routes, err := server.connectorAdapterRegistry().RouteDefinitions(kinds)
	if err != nil {
		panic(err)
	}
	for _, route := range routes {
		handler := route.Handler
		mux.HandleFunc(route.Pattern(), func(w http.ResponseWriter, r *http.Request) {
			handler(server.connectorPortsApplication().RouteGateway(), w, r)
		})
	}
}

func (s *Server) connectorCredentialProfileLifecycleAdapterFor(kind string) connectorapi.CredentialProfileLifecycleAdapter {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.CredentialProfileLifecycleAdapter)
	return adapter
}

func (s *Server) connectorCredentialProfileTesterFor(kind string) connectorapi.CredentialProfileTester {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.CredentialProfileTester)
	return adapter
}

func (s *Server) connectorTargetOperationRunnerFor(kind string) connectorapi.TargetOperationRunner {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.TargetOperationRunner)
	return adapter
}

func (s *Server) connectorCredentialCanonicalizerFor(kind string) connectorapi.CredentialCanonicalizer {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.CredentialCanonicalizer)
	return adapter
}

func (s *Server) connectorLiveConsoleTargetAdapterFor(kind string) connectorapi.LiveConsoleTargetAdapter {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.LiveConsoleTargetAdapter)
	return adapter
}

func (s *Server) connectorLiveConsoleTransportAdapterFor(kind string) connectorapi.LiveConsoleTransportAdapter {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.LiveConsoleTransportAdapter)
	return adapter
}

func (s *Server) connectorFileTransferAdapterFor(kind string) connectorapi.FileTransferAdapter {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.FileTransferAdapter)
	return adapter
}

func (s *Server) connectorCredentialResourceAdapterFor(kind string) connectorapi.CredentialResourceAdapter {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.CredentialResourceAdapter)
	return adapter
}
