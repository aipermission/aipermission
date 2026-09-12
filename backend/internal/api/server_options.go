package api

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

type serverOptions struct {
	registry           *connectors.Registry
	adapterRegistry    *connectorapi.Registry
	maintenanceConsole gatewayoperations.MaintenanceConsoleRuntime
}

type ServerOption func(*serverOptions)

func WithConnectorRegistry(registry *connectors.Registry) ServerOption {
	return func(options *serverOptions) { options.registry = registry }
}

func WithConnectorAdapterRegistry(registry *connectorapi.Registry) ServerOption {
	return func(options *serverOptions) { options.adapterRegistry = registry }
}

func WithMaintenanceConsole(runtime gatewayoperations.MaintenanceConsoleRuntime) ServerOption {
	return func(options *serverOptions) { options.maintenanceConsole = runtime }
}

func resolveServerOptions(options []ServerOption) serverOptions {
	resolved := serverOptions{
		registry: connectors.NewRegistry(), adapterRegistry: connectorapi.NewRegistry(),
	}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	if resolved.registry == nil {
		resolved.registry = connectors.NewRegistry()
	}
	if resolved.adapterRegistry == nil {
		resolved.adapterRegistry = connectorapi.NewRegistry()
	}
	return resolved
}
