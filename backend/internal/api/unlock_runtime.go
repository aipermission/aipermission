package api

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

func (s *Server) isUnlocked() bool {
	if s.workspaceState.Registry == nil {
		return false
	}
	return s.workspaceState.Registry.IsUnlocked()
}

func (s *Server) workspaceSelection() gatewayworkspace.Identity {
	if s.workspaceState.Registry == nil {
		return gatewayworkspace.Identity{
			ID: gatewayworkspace.DefaultID(s.config.DataPath), Path: s.config.DataPath,
		}
	}
	return s.workspaceState.Registry.Selection()
}

func (s *Server) openRuntimeForLifecycle(path string, id string, password string) (*databaseRuntime, error) {
	if s.workspaceState.OpenRuntime != nil {
		return s.workspaceState.OpenRuntime(path, id, password)
	}
	return s.openRuntime(path, id, password)
}

func (s *Server) moveDatabase(currentPath string, targetPath string) error {
	if s.workspaceState.MoveDatabase != nil {
		return s.workspaceState.MoveDatabase(currentPath, targetPath)
	}
	return gatewayworkspace.Move(currentPath, targetPath)
}

func (s *Server) publishDatabase(sourcePath string, targetPath string) error {
	if s.workspaceState.PublishDatabase != nil {
		return s.workspaceState.PublishDatabase(sourcePath, targetPath)
	}
	return gatewayworkspace.Publish(sourcePath, targetPath)
}

func (s *Server) openRuntime(path string, id string, password string) (*databaseRuntime, error) {
	runtime, err := gatewayworkspace.Open(context.Background(), gatewayworkspace.OpenInput{
		ID: id, Path: path, Password: password,
		ConfiguredGatewaySecret: s.config.GatewaySecret,
		Registry:                s.connectorRegistry(), AdapterRegistry: s.connectorAdapterRegistry(),
	})
	if err != nil {
		return nil, err
	}
	if err := s.reconcileConnectorRuntimeSurfaces(context.Background(), runtime); err != nil {
		s.discardOpeningRuntime(runtime)
		return nil, fmt.Errorf("reconcile connector runtime surfaces: %w", err)
	}
	settings, err := runtime.Security.Policy.ReadSettings(context.Background())
	if err != nil {
		s.discardOpeningRuntime(runtime)
		return nil, err
	}
	runtime.Security.Runtime.SetMCPStarted(settings.MCPStartEnabled)
	runtime.Connectors.ConsoleSessions = console.NewManager(runtime.Storage.Database, s.runtimeConsoleOpener(runtime), s.runtimeRedactor(runtime))
	if err := s.initializeCommandRequestRuntime(runtime); err != nil {
		s.discardOpeningRuntime(runtime)
		return nil, fmt.Errorf("initialize command request runtime: %w", err)
	}
	if err := s.initializeFileTransferRuntime(runtime); err != nil {
		s.discardOpeningRuntime(runtime)
		return nil, fmt.Errorf("initialize file transfer runtime: %w", err)
	}
	if err := s.configureVaultSessionRuntime(runtime); err != nil {
		s.discardOpeningRuntime(runtime)
		return nil, fmt.Errorf("initialize Vault session runtime: %w", err)
	}
	s.configureAuditDispatcher(runtime)
	return runtime, nil
}

func (s *Server) discardOpeningRuntime(runtime *databaseRuntime) {
	if err := gatewayworkspace.Discard(runtime); err != nil {
		log.Printf("discard opening workspace runtime failed workspace=%s error=%v", runtime.ID, err)
	}
}

func (s *Server) currentDataPath() string {
	return s.workspaceSelection().Path
}

func (s *Server) unlockedRuntimeSnapshot() []*databaseRuntime {
	if s.workspaceState.Lifecycle != nil {
		return s.workspaceState.Lifecycle.Snapshot()
	}
	return s.workspaceState.Registry.Snapshot()
}

func (s *Server) activeRuntime() *databaseRuntime {
	if s.workspaceState.Registry == nil {
		return nil
	}
	if s.workspaceState.Lifecycle != nil {
		runtime, _ := s.workspaceState.Lifecycle.Active()
		return runtime
	}
	runtime, _ := s.workspaceState.Registry.Active()
	return runtime
}

func (s *Server) closeRuntime(runtime *databaseRuntime) error {
	return gatewayworkspace.Close(runtime, func() (gatewayworkspace.ActionWorkflow, error) {
		return s.connectorActionWorkflow(runtime)
	})
}

func rejectPlaintextDatabase(w http.ResponseWriter, path string) bool {
	if !gatewayworkspace.LooksPlaintext(path) {
		return false
	}
	writeError(w, http.StatusConflict, "plaintext SQLite databases are not supported; create or import an encrypted .aipdb database")
	return true
}
