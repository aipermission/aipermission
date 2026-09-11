package api

import (
	"context"
	"fmt"
	"log"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) isUnlocked() bool {
	return s != nil && s.infrastructure != nil && s.infrastructure.WorkspaceIsUnlocked()
}

func (s *Server) workspaceSelection() gatewayinfra.Identity {
	if s == nil {
		return gatewayinfra.Identity{}
	}
	if s.infrastructure == nil {
		return gatewayinfra.Identity{Path: s.config.DataPath}
	}
	return s.infrastructure.WorkspaceSelection()
}

func (s *Server) openRuntimeForLifecycle(path string, id string, password string) (databaseRuntime, error) {
	if s.openRuntimeOverride != nil {
		return s.openRuntimeOverride(path, id, password)
	}
	return s.openRuntime(path, id, password)
}

func (s *Server) moveDatabase(currentPath string, targetPath string) error {
	if s.moveDatabaseOverride != nil {
		return s.moveDatabaseOverride(currentPath, targetPath)
	}
	return s.infrastructure.MoveDatabase(currentPath, targetPath)
}

func (s *Server) publishDatabase(sourcePath string, targetPath string) error {
	if s.publishDatabaseOverride != nil {
		return s.publishDatabaseOverride(sourcePath, targetPath)
	}
	return s.infrastructure.PublishDatabase(sourcePath, targetPath)
}

func (s *Server) openRuntime(path string, id string, password string) (databaseRuntime, error) {
	runtime, err := s.infrastructure.OpenWorkspace(context.Background(), gatewayinfra.OpenInput{
		ID: id, Path: path, Password: password,
		ConfiguredGatewaySecret: s.config.GatewaySecret,
		Registry:                s.connectorRegistry(), AdapterRegistry: s.connectorAdapterRegistry(),
	})
	if err != nil {
		return nil, err
	}
	if err := s.initializeOpenedRuntime(context.Background(), runtime); err != nil {
		s.discardOpeningRuntime(runtime)
		return nil, err
	}
	return runtime, nil
}

func (s *Server) initializeOpenedRuntime(ctx context.Context, runtime databaseRuntime) error {
	if runtime == nil {
		return fmt.Errorf("initialize workspace runtime: runtime is unavailable")
	}
	if err := s.reconcileConnectorRuntimeSurfaces(ctx, runtime); err != nil {
		return fmt.Errorf("reconcile connector runtime surfaces: %w", err)
	}
	settings, err := runtime.SecurityPort().PolicyService().ReadSettings(ctx)
	if err != nil {
		return fmt.Errorf("read workspace security settings: %w", err)
	}
	runtime.SecurityPort().RuntimeControlState().SetMCPStarted(settings.MCPStartEnabled)
	runtime.ConnectorPort().ConfigureConsoleSessions(s.runtimeConsoleOpener(runtime), s.runtimeRedactor(runtime))
	if err := s.initializeCommandRequestRuntime(runtime); err != nil {
		return fmt.Errorf("initialize command request runtime: %w", err)
	}
	if err := s.initializeFileTransferRuntime(runtime); err != nil {
		return fmt.Errorf("initialize file transfer runtime: %w", err)
	}
	if err := s.configureVaultSessionRuntime(runtime); err != nil {
		return fmt.Errorf("initialize Vault session runtime: %w", err)
	}
	s.configureAuditDispatcher(runtime)
	return nil
}

func (s *Server) discardOpeningRuntime(runtime databaseRuntime) {
	if err := s.infrastructure.DiscardWorkspace(runtime, func() gatewayinfra.TransferWorkflow {
		return s.transfers.Lifecycle(runtime)
	}); err != nil {
		log.Printf("discard opening workspace runtime failed workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
	}
	s.releaseRuntimeApplications(runtime)
}

func (s *Server) currentDataPath() string {
	return s.workspaceSelection().Path
}

func (s *Server) unlockedRuntimeSnapshot() []databaseRuntime {
	return s.infrastructure.WorkspaceSnapshot()
}

func (s *Server) activeRuntime() databaseRuntime {
	if s == nil || s.infrastructure == nil {
		return nil
	}
	return s.infrastructure.ActiveWorkspace()
}

func (s *Server) closeRuntime(runtime databaseRuntime) error {
	defer s.releaseRuntimeApplications(runtime)
	return s.infrastructure.CloseWorkspace(runtime, func() (gatewayinfra.ActionWorkflow, error) {
		return s.connectorActionShutdownWorkflow(runtime)
	}, func() (gatewayinfra.CommandWorkflow, error) {
		return s.commandRuntime(runtime)
	}, func() gatewayinfra.TransferWorkflow {
		return s.transfers.Lifecycle(runtime)
	})
}

func (s *Server) releaseRuntimeApplications(runtime databaseRuntime) {
	if s == nil || runtime == nil {
		return
	}
	s.access.ReleaseCommandRuntime(runtime.RuntimeIdentifier())
	s.connectorActions.ReleaseWorkspace(s.connectorActionWorkspace(runtime))
	s.vault.ReleaseWorkspace(s.vaultRuntime(runtime))
}
