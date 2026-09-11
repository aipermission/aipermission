// Package gatewayoptions owns gateway construction options and their defaults.
package gatewayoptions

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type Options struct {
	Registry                   *connectors.Registry
	AdapterRegistry            *connectorapi.Registry
	MaintenanceConsole         console.MaintenanceConsoleRuntime
	RuntimeInstanceIDGenerator func() (string, error)
}

type ConnectorRegistry = connectors.Registry
type ConnectorAdapterRegistry = connectorapi.Registry
type MaintenanceConsoleRuntime = console.MaintenanceConsoleRuntime

type Option func(*Options)

func WithConnectorRegistry(registry *ConnectorRegistry) Option {
	return func(options *Options) { options.Registry = registry }
}

func WithConnectorAdapterRegistry(registry *ConnectorAdapterRegistry) Option {
	return func(options *Options) { options.AdapterRegistry = registry }
}

func WithMaintenanceConsole(runtime MaintenanceConsoleRuntime) Option {
	return func(options *Options) { options.MaintenanceConsole = runtime }
}

func WithRuntimeInstanceIDGenerator(generator func() (string, error)) Option {
	return func(options *Options) { options.RuntimeInstanceIDGenerator = generator }
}

func Resolve(options []Option) Options {
	resolved := Options{
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
