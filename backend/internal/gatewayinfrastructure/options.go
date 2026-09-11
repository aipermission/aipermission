package gatewayinfrastructure

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type componentOptions struct {
	Registry                   *connectors.Registry
	AdapterRegistry            *connectorapi.Registry
	MaintenanceConsole         console.MaintenanceConsoleRuntime
	RuntimeInstanceIDGenerator func() (string, error)
}

type ServerOption func(*componentOptions)

func WithConnectorRegistry(registry *connectors.Registry) ServerOption {
	return func(options *componentOptions) { options.Registry = registry }
}

func WithConnectorAdapterRegistry(registry *connectorapi.Registry) ServerOption {
	return func(options *componentOptions) { options.AdapterRegistry = registry }
}

func WithMaintenanceConsole(runtime console.MaintenanceConsoleRuntime) ServerOption {
	return func(options *componentOptions) { options.MaintenanceConsole = runtime }
}

func WithRuntimeInstanceIDGenerator(generator func() (string, error)) ServerOption {
	return func(options *componentOptions) { options.RuntimeInstanceIDGenerator = generator }
}

func resolveOptions(options []ServerOption) componentOptions {
	resolved := componentOptions{
		Registry: connectors.NewRegistry(), AdapterRegistry: connectorapi.NewRegistry(),
		RuntimeInstanceIDGenerator: executionprincipal.NewRuntimeInstanceID,
	}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	if resolved.Registry == nil {
		resolved.Registry = connectors.NewRegistry()
	}
	if resolved.AdapterRegistry == nil {
		resolved.AdapterRegistry = connectorapi.NewRegistry()
	}
	if resolved.RuntimeInstanceIDGenerator == nil {
		resolved.RuntimeInstanceIDGenerator = executionprincipal.NewRuntimeInstanceID
	}
	return resolved
}
