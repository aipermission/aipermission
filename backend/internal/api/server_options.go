package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

type serverOptions struct {
	registry                   *connectors.Registry
	adapterRegistry            *connectorapi.Registry
	maintenanceConsole         gatewayoperations.MaintenanceConsoleRuntime
	runtimeInstanceIDGenerator func() (string, error)
	openWorkspace              func(context.Context, string, string, string) (*gatewayinfra.WorkspaceHandle, error)
	moveDatabase               func(string, string) error
	publishDatabase            func(string, string) error
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

func withRuntimeInstanceIDGenerator(generator func() (string, error)) ServerOption {
	return func(options *serverOptions) { options.runtimeInstanceIDGenerator = generator }
}

func withWorkspaceOpen(open func(context.Context, string, string, string) (*gatewayinfra.WorkspaceHandle, error)) ServerOption {
	return func(options *serverOptions) { options.openWorkspace = open }
}

func withDatabaseMove(move func(string, string) error) ServerOption {
	return func(options *serverOptions) { options.moveDatabase = move }
}

func withDatabasePublish(publish func(string, string) error) ServerOption {
	return func(options *serverOptions) { options.publishDatabase = publish }
}

func resolveServerOptions(options []ServerOption) serverOptions {
	resolved := serverOptions{
		registry: connectors.NewRegistry(), adapterRegistry: connectorapi.NewRegistry(),
		runtimeInstanceIDGenerator: gatewayaccess.NewRuntimeInstanceID,
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
	if resolved.runtimeInstanceIDGenerator == nil {
		resolved.runtimeInstanceIDGenerator = gatewayaccess.NewRuntimeInstanceID
	}
	return resolved
}
