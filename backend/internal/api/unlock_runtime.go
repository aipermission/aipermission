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

func (s *Server) openRuntime(ctx context.Context, path string, id string, password string) (*gatewayinfra.WorkspaceHandle, error) {
	runtime, err := s.workspaceOwner.OpenWorkspace(ctx, gatewayinfra.NewOpenWorkspaceInput(
		id, path, password, s.config.GatewaySecret, s.connectorRegistry(), s.connectorAdapterRegistry(),
	))
	if err != nil {
		return nil, err
	}
	if err := s.initializeOpenedRuntime(ctx, runtime); err != nil {
		return nil, errors.Join(err, s.discardOpeningRuntime(runtime))
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, s.discardOpeningRuntime(runtime))
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
	if err := s.initializeFileTransferRuntime(ctx, runtime); err != nil {
		return fmt.Errorf("initialize file transfer runtime: %w", err)
	}
	if err := s.configureVaultSessionRuntime(runtime); err != nil {
		return fmt.Errorf("initialize Vault session runtime: %w", err)
	}
	s.configureAuditDispatcher(runtime)
	return nil
}

func (s *Server) discardOpeningRuntime(runtime *gatewayinfra.WorkspaceHandle) error {
	err := s.workspaceOwner.DiscardWorkspace(runtime, func() gatewayinfra.TransferWorkflow {
		return s.operationsOwner.TransferLifecycle(runtime)
	}, func() { s.releaseRuntimeApplications(runtime) })
	if err != nil {
		log.Printf("discard opening workspace runtime failed workspace=%s error=%v", runtime.Identity().DatabaseID, err)
	}
	return err
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
		return s.operationsOwner.TransferLifecycle(runtime)
	}, func() { s.releaseRuntimeApplications(runtime) })
}

func (s *Server) releaseRuntimeApplications(runtime *gatewayinfra.WorkspaceHandle) {
	if s == nil || runtime == nil {
		return
	}
	s.commands.Release(runtime.Identity().RuntimeID)
	s.connectorActions.Release(runtime)
	s.vaultOwner.ReleaseVaultWorkspace(runtime, s.vaultApplication(), s.vaultRuntimePorts(runtime))
}
