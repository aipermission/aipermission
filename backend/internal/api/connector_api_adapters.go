package api

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func connectorAPIAdapterFor(kind string) connectorapi.Adapter {
	return connectorapi.For(kind)
}

func connectorRuntimeAdapterFor(kind string) connectorapi.RuntimeAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.RuntimeAdapter)
	return adapter
}

func connectorRuntimeServices(kind string, server *Server, runtime *databaseRuntime) map[string]any {
	adapter := connectorRuntimeAdapterFor(kind)
	if adapter == nil {
		return nil
	}
	return adapter.RuntimeServices(server, runtime)
}

func registerConnectorAdapterRoutes(mux any, server *Server) {
	for _, info := range server.connectorRegistry().List() {
		adapter, _ := connectorAPIAdapterFor(info.Kind).(connectorapi.RouteRegistrar)
		if adapter != nil {
			adapter.RegisterRoutes(mux, server)
		}
	}
}

func connectorRuntimeResources(registrySource *connectors.Registry, database any, vault any) map[string]any {
	resources := map[string]any{}
	if registrySource == nil {
		return resources
	}
	for _, info := range registrySource.List() {
		provider, _ := connectorAPIAdapterFor(info.Kind).(connectorapi.RuntimeResourceProvider)
		if provider == nil {
			continue
		}
		for name, value := range provider.RuntimeResources(database, vault) {
			if name == "" || value == nil {
				continue
			}
			resources[info.Kind+"/"+name] = value
		}
	}
	return resources
}

func connectorDraftTesterFor(kind string) connectorapi.DraftTester {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.DraftTester)
	return adapter
}

func connectorTargetDeleterFor(kind string) connectorapi.TargetDeleter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.TargetDeleter)
	return adapter
}

func connectorCredentialProfileLifecycleAdapterFor(kind string) connectorapi.CredentialProfileLifecycleAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.CredentialProfileLifecycleAdapter)
	return adapter
}

func connectorCredentialProfileTesterFor(kind string) connectorapi.CredentialProfileTester {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.CredentialProfileTester)
	return adapter
}

func connectorTargetOperationRunnerFor(kind string) connectorapi.TargetOperationRunner {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.TargetOperationRunner)
	return adapter
}

func connectorCredentialCanonicalizerFor(kind string) connectorapi.CredentialCanonicalizer {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.CredentialCanonicalizer)
	return adapter
}

func connectorLiveConsoleTargetAdapterFor(kind string) connectorapi.LiveConsoleTargetAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.LiveConsoleTargetAdapter)
	return adapter
}

func connectorLiveConsoleTransportAdapterFor(kind string) connectorapi.LiveConsoleTransportAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.LiveConsoleTransportAdapter)
	return adapter
}

func connectorFileTransferAdapterFor(kind string) connectorapi.FileTransferAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.FileTransferAdapter)
	return adapter
}

func connectorCredentialResourceAdapterFor(kind string) connectorapi.CredentialResourceAdapter {
	adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.CredentialResourceAdapter)
	return adapter
}
