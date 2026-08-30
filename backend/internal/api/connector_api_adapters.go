package api

import (
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func (s *Server) connectorAPIAdapterFor(kind string) connectorapi.Adapter {
	return s.connectorAdapterRegistry().For(kind)
}

func (runtime *databaseRuntime) connectorAPIAdapterFor(kind string) connectorapi.Adapter {
	return runtime.connectorAdapterRegistry().For(kind)
}

func (s *Server) connectorRuntimeAdapterFor(kind string) connectorapi.RuntimeAdapter {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.RuntimeAdapter)
	return adapter
}

type connectorRuntimeCapabilities map[string]connectors.RuntimeCapability

func (c connectorRuntimeCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	return c[name]
}

func connectorRuntimeCapabilitiesFor(kind string, server *Server, runtime *databaseRuntime) connectors.RuntimeCapabilityResolver {
	capabilities := connectorRuntimeCapabilities{}
	if server != nil && runtime != nil {
		networkTransport := connectorNetworkTransport{server: server, runtime: runtime}
		capabilities[networkTransport.ConnectorRuntimeCapability()] = networkTransport
		commandTransport := connectorCommandTransport{server: server, runtime: runtime}
		capabilities[commandTransport.ConnectorRuntimeCapability()] = commandTransport
	}
	adapter := server.connectorRuntimeAdapterFor(kind)
	if adapter != nil {
		for name, capability := range adapter.RuntimeCapabilities(server, runtime) {
			if name == "" || capability == nil {
				continue
			}
			capabilities[name] = capability
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
			handler(server, w, r)
		})
	}
}

func connectorRuntimeResources(registrySource *connectors.Registry, adapterRegistry *connectorapi.Registry, database *sql.DB, secretVault *vault.Vault) map[string]any {
	resources := map[string]any{}
	if registrySource == nil {
		return resources
	}
	for _, info := range registrySource.List() {
		provider, _ := adapterRegistry.For(info.Kind).(connectorapi.RuntimeResourceProvider)
		if provider == nil {
			continue
		}
		for name, value := range provider.RuntimeResources(database, secretVault) {
			if name == "" || value == nil {
				continue
			}
			resources[info.Kind+"/"+name] = value
		}
	}
	return resources
}

func (s *Server) connectorDraftTesterFor(kind string) connectorapi.DraftTester {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.DraftTester)
	return adapter
}

func (s *Server) connectorTargetDeleterFor(kind string) connectorapi.TargetDeleter {
	adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.TargetDeleter)
	return adapter
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
