package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/filetransfer/httpapi"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

var (
	errDatabaseAuthentication = errors.New("database authentication failed")
	errDatabaseInitialization = errors.New("database initialization failed")
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

func (s *Server) currentUnlockStatus() (unlockStatusResponse, error) {
	status, err := s.workspaceLifecycle.Status()
	if err != nil {
		return unlockStatusResponse{}, err
	}
	return unlockStatusFromLifecycle(status), nil
}

func (s *Server) currentUnlockStatusLocked() (unlockStatusResponse, error) {
	return s.currentUnlockStatus()
}

func unlockStatusFromLifecycle(status workspacelifecycle.Status) unlockStatusResponse {
	return unlockStatusResponse{
		State: status.State, DataPath: status.Identity.Path, DatabaseID: status.Identity.ID,
		DatabaseName: status.DatabaseName, DatabaseSizeBytes: status.DatabaseSizeBytes,
		UISessionAuthenticated: status.State == "unlocked", Databases: status.Databases,
	}
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
	ownership, err := db.AcquireDatabaseOwnership(path)
	if err != nil {
		return nil, err
	}
	owned := true
	defer func() {
		if owned {
			_ = ownership.Close()
		}
	}()
	existingDatabase := db.Exists(path)
	snapshotsBeforeOpen := preMigrationSnapshotSet(path)
	if existingDatabase {
		if err := db.ValidateEncrypted(path, password); err != nil {
			return nil, fmt.Errorf("%w: encrypted database validation failed", errDatabaseAuthentication)
		}
	}
	runtime, err := s.openValidatedRuntime(path, id, password)
	if runtime != nil {
		runtime.databaseOwnership = ownership
		owned = false
	}
	if err == nil || !existingDatabase {
		return runtime, err
	}
	return nil, databaseInitializationError(path, err, snapshotsBeforeOpen)
}

func (s *Server) openValidatedRuntime(path string, id string, password string) (*databaseRuntime, error) {
	database, err := db.OpenEncrypted(path, password)
	if err != nil {
		return nil, err
	}
	bindingRequired, err := recordcrypto.EnvelopeMarkerPresent(context.Background(), database)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	gatewaySecret, err := projectvault.ResolveGatewaySecret(context.Background(), database, s.config.GatewaySecret)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	secretVault, err := vault.New(gatewaySecret)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	workspaceUUID, err := workspaceUUIDFromDatabase(context.Background(), database, bindingRequired)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	uiRetryIdentity, err := projectvault.EnsureUIRetryIdentity(context.Background(), database)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	if _, err := recordcrypto.RewriteLegacy(context.Background(), database, secretVault, workspaceUUID); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate encrypted records: %w", err)
	}
	actionIdentityKey, err := actions.DeriveIdentityKey(gatewaySecret, workspaceUUID)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	runtimeInstanceID, err := executionprincipal.NewRuntimeInstanceID()
	if err != nil {
		actions.ClearIdentityKey(actionIdentityKey)
		_ = database.Close()
		return nil, err
	}
	runtime := &databaseRuntime{
		id:                 id,
		path:               path,
		gatewaySecret:      gatewaySecret,
		database:           database,
		vault:              secretVault,
		tokens:             tokens.NewEncryptedStore(database, secretVault, workspaceUUID),
		registry:           s.connectorRegistry(),
		adapterRegistry:    s.connectorAdapterRegistry(),
		connectorResources: connectorruntime.NewResourceScopes(database, secretVault, workspaceUUID),
		workspaceUUID:      workspaceUUID,
		uiRetryIdentity:    uiRetryIdentity,
		runtimeInstanceID:  runtimeInstanceID,
		actionIdentityKey:  actionIdentityKey,
		vaultLeases:        vaultsessions.NewStore(),
		securityPolicy:     securitypolicy.NewService(database),
	}
	runtime.transferLifecycle = filetransferhttp.NewLifecycle()
	if err := s.reconcileConnectorRuntimeSurfaces(context.Background(), runtime); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("reconcile connector runtime surfaces: %w", err)
	}
	settings, err := runtime.securityPolicy.ReadSettings(context.Background())
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	runtime.runtimeState.SetMCPStarted(settings.MCPStartEnabled)
	runtime.consoleSessions = console.NewManager(database, s.runtimeConsoleOpener(runtime), s.runtimeRedactor(runtime))
	if err := s.initializeCommandRequestRuntime(runtime); err != nil {
		runtime.transferLifecycle.Stop()
		actions.ClearIdentityKey(actionIdentityKey)
		_ = database.Close()
		return nil, fmt.Errorf("initialize command request runtime: %w", err)
	}
	if err := s.initializeFileTransferRuntime(runtime); err != nil {
		runtime.transferLifecycle.Stop()
		actions.ClearIdentityKey(actionIdentityKey)
		_ = database.Close()
		return nil, fmt.Errorf("initialize file transfer runtime: %w", err)
	}
	if err := s.configureVaultSessionRuntime(runtime); err != nil {
		runtime.transferLifecycle.Stop()
		actions.ClearIdentityKey(actionIdentityKey)
		_ = database.Close()
		return nil, fmt.Errorf("initialize Vault session runtime: %w", err)
	}
	s.configureAuditDispatcher(runtime)
	return runtime, nil
}

func databaseInitializationError(path string, cause error, snapshotsBeforeOpen map[string]struct{}) error {
	err := fmt.Errorf("%w: %w", errDatabaseInitialization, cause)
	matches, globErr := filepath.Glob(path + ".pre-migration-v*.aipdb")
	if globErr != nil {
		return err
	}
	created := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, existed := snapshotsBeforeOpen[match]; !existed {
			created = append(created, match)
		}
	}
	if len(created) == 0 {
		return err
	}
	sort.Strings(created)
	return fmt.Errorf("%w; encrypted pre-migration snapshot retained at %s", err, created[len(created)-1])
}

func preMigrationSnapshotSet(path string) map[string]struct{} {
	matches, _ := filepath.Glob(path + ".pre-migration-v*.aipdb")
	set := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		set[match] = struct{}{}
	}
	return set
}

func workspaceUUIDFromDatabase(ctx context.Context, database *sql.DB, bindingRequired bool) (string, error) {
	if bindingRequired {
		return projectvault.ReadWorkspaceUUID(ctx, database)
	}
	return projectvault.EnsureWorkspaceUUID(ctx, database)
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
	if runtime.vaultLeases != nil {
		runtime.vaultLeases.Clear()
	}
	if runtime.consoleSessions != nil {
		runtime.consoleSessions.CloseAll()
	}
	if runtime.commandRequests != nil {
		if err := runtime.commandRequests.CancelRunning(context.Background(), "workspace locked while command was running"); err != nil {
			log.Printf("mark running command requests failed workspace=%s error=%v", runtime.id, err)
		}
	}
	if err := s.markRunningConnectorActionsOutcomeUnknown(runtime); err != nil {
		log.Printf("mark running connector actions outcome unknown failed workspace=%s error=%v", runtime.id, err)
	}
	if runtime.fileTransfers == nil {
		runtime.transferLifecycle.Stop()
		log.Printf("file transfer shutdown runtime unavailable workspace=%s", runtime.id)
	} else {
		drained, err := s.fileTransferHTTPHandlers().ShutdownRuntime(
			runtime.fileTransfers,
			fileTransferShutdownWait,
			"workspace locked while file transfer was running",
			"workspace locked while file transfer queue was running",
		)
		if err != nil {
			log.Printf("mark running file transfers failed workspace=%s error=%v", runtime.id, err)
		}
		if !drained {
			go func() {
				runtime.transferLifecycle.Wait(context.Background())
				if err := closeRuntimeStorage(runtime); err != nil {
					log.Printf("deferred runtime storage close failed workspace=%s error=%v", runtime.id, err)
				}
			}()
			return fmt.Errorf("file transfer shutdown exceeded %s; runtime storage close deferred until workers exit", fileTransferShutdownWait)
		}
	}
	return closeRuntimeStorage(runtime)
}

func closeRuntimeStorage(runtime *databaseRuntime) error {
	if runtime.auditDispatcher != nil {
		runtime.auditDispatcher.Stop()
	}
	actions.ClearIdentityKey(runtime.actionIdentityKey)
	runtime.actionIdentityKey = nil
	var closeErrors []error
	if runtime.database != nil {
		if err := runtime.database.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close encrypted database runtime %q: %w", runtime.id, err))
		}
	}
	if runtime.databaseOwnership != nil {
		if err := runtime.databaseOwnership.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("release encrypted database runtime %q ownership: %w", runtime.id, err))
		}
		runtime.databaseOwnership = nil
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

func validateUnlockPassword(password string, confirm string) error {
	if len(password) < 14 {
		return fmt.Errorf("password must be at least 14 characters")
	}
	if password != confirm {
		return errPasswordMismatch{}
	}
	var hasUpper, hasLower, hasDigit bool
	for _, char := range password {
		switch {
		case char >= 'A' && char <= 'Z':
			hasUpper = true
		case char >= 'a' && char <= 'z':
			hasLower = true
		case char >= '0' && char <= '9':
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("password must include uppercase letters, lowercase letters, and numbers")
	}
	return nil
}

func clearStringReferences(values ...*string) {
	for _, value := range values {
		if value != nil {
			// Best-effort reference clearing only. Go strings are immutable, so
			// this does not guarantee heap zeroization of already-decoded JSON
			// input; it prevents keeping extra request references alive.
			*value = ""
		}
	}
}

type errPasswordMismatch struct{}

func (errPasswordMismatch) Error() string {
	return "password confirmation does not match"
}
