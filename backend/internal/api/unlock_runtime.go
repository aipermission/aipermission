package api

import (
	"context"
	"errors"
	"fmt"
	"log"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) isUnlocked() bool {
	return s != nil && s.workspaceOwner != nil && s.workspaceOwner.WorkspaceIsUnlocked()
}

func (s *Server) workspaceSelection() gatewayinfra.Identity {
	if s == nil {
		return gatewayinfra.Identity{}
	}
	if s.workspaceOwner == nil {
		return gatewayinfra.Identity{Path: s.config.DataPath}
	}
	return s.workspaceOwner.WorkspaceSelection()
}

func (s *Server) openRuntimeForLifecycle(path string, id string, password string) (*gatewayinfra.WorkspaceHandle, error) {
	if s.openRuntimeOverride != nil {
		return s.openRuntimeOverride(path, id, password)
	}
	return s.openRuntime(path, id, password)
}

func (s *Server) moveDatabase(currentPath string, targetPath string) error {
	if s.moveDatabaseOverride != nil {
		return s.moveDatabaseOverride(currentPath, targetPath)
	}
	return s.workspaceOwner.MoveDatabase(currentPath, targetPath)
}

func (s *Server) publishDatabase(sourcePath string, targetPath string) error {
	if s.publishDatabaseOverride != nil {
		return s.publishDatabaseOverride(sourcePath, targetPath)
	}
	return s.workspaceOwner.PublishDatabase(sourcePath, targetPath)
}

func (s *Server) openRuntime(path string, id string, password string) (*gatewayinfra.WorkspaceHandle, error) {
	runtime, err := s.workspaceOwner.OpenWorkspace(context.Background(), gatewayinfra.NewOpenWorkspaceInput(
		id, path, password, s.config.GatewaySecret, s.connectorRegistry(), s.connectorAdapterRegistry(),
	))
	if err != nil {
		return nil, err
	}
	if err := s.initializeOpenedRuntime(context.Background(), runtime); err != nil {
		s.discardOpeningRuntime(runtime)
		return nil, err
	}
	return runtime, nil
}

func (s *Server) initializeOpenedRuntime(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) error {
	if runtime == nil {
		return fmt.Errorf("initialize workspace runtime: runtime is unavailable")
	}
	if err := s.reconcileConnectorRuntimeSurfaces(ctx, runtime); err != nil {
		return fmt.Errorf("reconcile connector runtime surfaces: %w", err)
	}
	if err := s.accessOwner.ConfigureWorkspaceRuntime(ctx, runtime, s.runtimeConsoleOpener(runtime)); err != nil {
		return fmt.Errorf("configure workspace security runtime: %w", err)
	}
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

func (s *Server) discardOpeningRuntime(runtime *gatewayinfra.WorkspaceHandle) {
	if err := s.workspaceOwner.DiscardWorkspace(runtime, func() gatewayinfra.TransferWorkflow {
		return s.transfers.Lifecycle(fileTransferWorkspaceIdentity(runtime))
	}); err != nil {
		log.Printf("discard opening workspace runtime failed workspace=%s error=%v", runtime.Identity().DatabaseID, err)
	}
	s.releaseRuntimeApplications(runtime)
}

func (s *Server) currentDataPath() string {
	return s.workspaceSelection().Path
}

func (s *Server) unlockedRuntimeSnapshot() []*gatewayinfra.WorkspaceHandle {
	return s.workspaceOwner.WorkspaceSnapshot()
}

func (s *Server) activeRuntime() *gatewayinfra.WorkspaceHandle {
	if s == nil || s.workspaceOwner == nil {
		return nil
	}
	return s.workspaceOwner.ActiveWorkspace()
}

func (s *Server) closeRuntime(runtime *gatewayinfra.WorkspaceHandle) error {
	return s.workspaceOwner.CloseWorkspace(runtime, func() (gatewayinfra.ActionWorkflow, error) {
		return s.connectorActionShutdownWorkflow(runtime)
	}, func() (gatewayinfra.CommandWorkflow, error) {
		workflow, err := s.commandRuntime(runtime)
		if errors.Is(err, gatewayoperations.ErrCommandRuntimeUnavailable) {
			return nil, nil
		}
		return workflow, err
	}, func() gatewayinfra.TransferWorkflow {
		return s.transfers.Lifecycle(fileTransferWorkspaceIdentity(runtime))
	}, func() { s.releaseRuntimeApplications(runtime) })
}

func (s *Server) releaseRuntimeApplications(runtime *gatewayinfra.WorkspaceHandle) {
	if s == nil || runtime == nil {
		return
	}
	s.commands.Release(runtime.Identity().RuntimeID)
	s.connectorActions.ReleaseWorkspace(s.connectorActionWorkspace(runtime))
	s.vault.ReleaseWorkspace(s.vaultRuntime(runtime))
}
