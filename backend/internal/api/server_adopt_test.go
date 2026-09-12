package api

import (
	"context"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

// NewServer adopts an already opened workspace for package-level integration
// tests. Production always starts locked and opens storage through lifecycle.
func NewServer(configuration RuntimeConfiguration, adopted gatewayworkspace.AdoptInput, options ...ServerOption) (*Server, error) {
	cfg := snapshotRuntimeConfiguration(configuration)
	resolved := resolveServerOptions(options)
	infrastructure := gatewayinfra.NewComponent(cfg.DataPath, describeDatabaseRuntime)
	workspaceOwner := infrastructure.WorkspaceOwner()
	server := newServerComposition(cfg, resolved, infrastructure)
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		return nil, err
	}
	adopted.ID = workspaceOwner.WorkspaceSelection().ID
	adopted.Path = cfg.DataPath
	adopted.ConfiguredGatewaySecret = cfg.GatewaySecret
	adopted.Registry = resolved.registry
	adopted.AdapterRegistry = resolved.adapterRegistry
	adopted.RuntimeInstanceID = resolved.runtimeInstanceIDGenerator
	runtime, err := workspaceOwner.AdoptWorkspace(context.Background(), adopted)
	if err != nil {
		return nil, err
	}
	if err := server.initializeOpenedRuntime(context.Background(), runtime); err != nil {
		server.discardOpeningRuntime(runtime)
		return nil, err
	}
	server.workspaceOwner.ActivateWorkspace(runtime)
	server.initializeRetention(runtime)
	server.routes()
	return server, nil
}
