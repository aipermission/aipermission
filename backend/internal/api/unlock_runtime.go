package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
)

const fileTransferShutdownWait = 10 * time.Second

func (s *Server) isUnlocked() bool {
	if s.workspaces == nil {
		return false
	}
	return s.workspaces.IsUnlocked()
}

func (s *Server) workspaceSelection() workspacelifecycle.Identity {
	if s.workspaces == nil {
		return workspacelifecycle.Identity{
			ID: databasecatalog.DefaultDatabaseID(s.config.DataPath), Path: s.config.DataPath,
		}
	}
	return s.workspaces.Selection()
}

func (s *Server) openRuntimeForLifecycle(path string, id string, password string) (*databaseRuntime, error) {
	if s.runtimeOpen != nil {
		return s.runtimeOpen(path, id, password)
	}
	return s.openRuntime(path, id, password)
}

func (s *Server) moveDatabase(currentPath string, targetPath string) error {
	if s.databaseMove != nil {
		return s.databaseMove(currentPath, targetPath)
	}
	return databasecatalog.MoveDatabase(currentPath, targetPath)
}

func (s *Server) publishDatabase(sourcePath string, targetPath string) error {
	if s.databasePublish != nil {
		return s.databasePublish(sourcePath, targetPath)
	}
	return db.PublishFileNoReplace(sourcePath, targetPath)
}

func (s *Server) openRuntime(path string, id string, password string) (*databaseRuntime, error) {
	state, err := foundation.Open(context.Background(), foundation.OpenInput{
		ID: id, Path: path, Password: password,
		ConfiguredGatewaySecret: s.config.GatewaySecret,
		Registry:                s.connectorRegistry(), AdapterRegistry: s.connectorAdapterRegistry(),
	})
	if err != nil {
		return nil, err
	}
	runtime := workspaceruntime.New(state)
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
	runtime.Operations.TransferLifecycle.Stop()
	if err := closeRuntimeStorage(runtime); err != nil {
		log.Printf("discard opening workspace runtime failed workspace=%s error=%v", runtime.ID, err)
	}
}

func (s *Server) currentDataPath() string {
	return s.workspaceSelection().Path
}

func (s *Server) unlockedRuntimeSnapshot() []*databaseRuntime {
	if s.workspaceLifecycle != nil {
		return s.workspaceLifecycle.Snapshot()
	}
	return s.workspaces.Snapshot()
}

func (s *Server) activeRuntime() *databaseRuntime {
	if s.workspaces == nil {
		return nil
	}
	if s.workspaceLifecycle != nil {
		runtime, _ := s.workspaceLifecycle.Active()
		return runtime
	}
	runtime, _ := s.workspaces.Active()
	return runtime
}

func (s *Server) closeRuntime(runtime *databaseRuntime) error {
	s.stopRetention(runtime)
	s.stopConnectorActionRecoveryWorker(runtime)
	if runtime.Security.VaultLeases != nil {
		runtime.Security.VaultLeases.Clear()
	}
	if runtime.Connectors.ConsoleSessions != nil {
		runtime.Connectors.ConsoleSessions.CloseAll()
	}
	if runtime.Operations.CommandRequests != nil {
		if err := runtime.Operations.CommandRequests.CancelRunning(context.Background(), "workspace locked while command was running"); err != nil {
			log.Printf("mark running command requests failed workspace=%s error=%v", runtime.ID, err)
		}
	}
	if err := s.markRunningConnectorActionsOutcomeUnknown(runtime); err != nil {
		log.Printf("mark running connector actions outcome unknown failed workspace=%s error=%v", runtime.ID, err)
	}
	if runtime.Operations.FileTransfers == nil {
		runtime.Operations.TransferLifecycle.Stop()
		log.Printf("file transfer shutdown runtime unavailable workspace=%s", runtime.ID)
	} else {
		drained, err := s.fileTransferHTTPHandlers().ShutdownRuntime(
			runtime.Operations.FileTransfers,
			fileTransferShutdownWait,
			"workspace locked while file transfer was running",
			"workspace locked while file transfer queue was running",
		)
		if err != nil {
			log.Printf("mark running file transfers failed workspace=%s error=%v", runtime.ID, err)
		}
		if !drained {
			go func() {
				runtime.Operations.TransferLifecycle.Wait(context.Background())
				if err := closeRuntimeStorage(runtime); err != nil {
					log.Printf("deferred runtime storage close failed workspace=%s error=%v", runtime.ID, err)
				}
			}()
			return fmt.Errorf("file transfer shutdown exceeded %s; runtime storage close deferred until workers exit", fileTransferShutdownWait)
		}
	}
	return closeRuntimeStorage(runtime)
}

func closeRuntimeStorage(runtime *databaseRuntime) error {
	if runtime.Observation.AuditDispatcher != nil {
		runtime.Observation.AuditDispatcher.Stop()
	}
	actions.ClearIdentityKey(runtime.ActionIdentityKey)
	runtime.ActionIdentityKey = nil
	var closeErrors []error
	if runtime.Storage.Database != nil {
		if err := runtime.Storage.Database.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close encrypted database runtime %q: %w", runtime.ID, err))
		}
	}
	if runtime.Storage.Ownership != nil {
		if err := runtime.Storage.Ownership.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("release encrypted database runtime %q ownership: %w", runtime.ID, err))
		}
		runtime.Storage.Ownership = nil
	}
	return errors.Join(closeErrors...)
}

func (s *Server) markRunningConnectorActionsOutcomeUnknown(runtime *databaseRuntime) error {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return err
	}
	return workflow.MarkRunningOutcomeUnknown(context.Background(), db.ConnectorActionOutcomeUnknownMessage)
}

func rejectPlaintextDatabase(w http.ResponseWriter, path string) bool {
	if !db.LooksLikePlainSQLite(path) {
		return false
	}
	writeError(w, http.StatusConflict, "plaintext SQLite databases are not supported; create or import an encrypted .aipdb database")
	return true
}

func isAllowedWhileLocked(path string) bool {
	switch path {
	case "/health", "/api/status", "/api/unlock/status", "/api/unlock/setup", "/api/unlock", "/api/backup/import",
		"/api/backup/remote/list", "/api/backup/remote/restore", "/api/databases/delete-locked":
		return true
	default:
		return false
	}
}
