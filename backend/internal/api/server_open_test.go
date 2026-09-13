package api

import (
	"context"
	"errors"
	"fmt"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

// NewServer opens an encrypted fixture through the same owned workspace path
// used by production. It exists only in package-level integration tests.
func NewServer(configuration RuntimeConfiguration, fixture testOpenInput, options ...ServerOption) (*Server, error) {
	if fixture.Database == nil || fixture.Password == "" {
		return nil, errors.New("test workspace fixture is incomplete")
	}
	cfg := snapshotRuntimeConfiguration(configuration)
	path, err := databasePath(fixture.Database)
	if err != nil {
		return nil, err
	}
	cfg.DataPath = path
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
	runtime, err := workspaceOwner.OpenWorkspace(context.Background(), gatewayinfra.NewOpenWorkspaceInput(
		workspaceOwner.WorkspaceSelection().ID, cfg.DataPath, fixture.Password, cfg.GatewaySecret,
		resolved.registry, resolved.adapterRegistry,
	))
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
