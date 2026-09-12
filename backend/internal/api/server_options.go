package api

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

type serverOptions struct {
	registry           connectors.Catalog
	adapterRegistry    connectorapi.Catalog
	maintenanceConsole gatewayoperations.MaintenanceConsoleRuntime
	err                error
}

type ServerOption func(*serverOptions)

func WithConnectorRegistry(registry connectors.Catalog) ServerOption {
	return func(options *serverOptions) {
		if options.err != nil {
			return
		}
		options.registry, options.err = connectors.SnapshotCatalog(registry)
	}
}

func WithConnectorAdapterRegistry(registry connectorapi.Catalog) ServerOption {
	return func(options *serverOptions) {
		if options.err != nil {
			return
		}
		options.adapterRegistry, options.err = connectorapi.SnapshotCatalog(registry)
	}
}

func WithMaintenanceConsole(runtime gatewayoperations.MaintenanceConsoleRuntime) ServerOption {
	return func(options *serverOptions) { options.maintenanceConsole = runtime }
}

func resolveServerOptions(options []ServerOption) serverOptions {
	resolved := serverOptions{
		registry: connectors.NewRegistry().Snapshot(), adapterRegistry: connectorapi.NewRegistry().Snapshot(),
	}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	if resolved.registry == nil {
		resolved.registry = connectors.NewRegistry().Snapshot()
	}
	if resolved.adapterRegistry == nil {
		resolved.adapterRegistry = connectorapi.NewRegistry().Snapshot()
	}
	return resolved
}
