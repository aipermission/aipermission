package api

import (
	"context"
	"errors"
	"fmt"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

// NewServer adopts an already opened workspace for package-level integration
// tests. Production always starts locked and opens storage through lifecycle.
func NewServer(configuration RuntimeConfiguration, adopted gatewayworkspace.AdoptInput, options ...ServerOption) (*Server, error) {
	cfg := snapshotRuntimeConfiguration(configuration)
	resolved := resolveServerOptions(options)
	if resolved.err != nil {
		return nil, fmt.Errorf("snapshot connector catalog: %w", resolved.err)
	}
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
	runtime, err := workspaceOwner.AdoptWorkspace(context.Background(), adopted)
	if err != nil {
		return nil, err
	}
	if err := server.initializeOpenedRuntime(context.Background(), runtime); err != nil {
		return nil, errors.Join(err, server.discardOpeningRuntime(runtime))
	}
	server.workspaceOwner.ActivateWorkspace(runtime)
	server.initializeRetention(runtime)
	server.routes()
	return server, nil
}
