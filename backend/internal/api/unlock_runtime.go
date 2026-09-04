package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

var (
	errDatabaseAuthentication = errors.New("database authentication failed")
	errDatabaseInitialization = errors.New("database initialization failed")
)

func (s *Server) isUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.workspaces) > 0
}

func (s *Server) currentUnlockStatus() (unlockStatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentUnlockStatusLocked()
}

func (s *Server) currentUnlockStatusLocked() (unlockStatusResponse, error) {
	databases, err := db.ListDatabases(s.config.DataPath, s.activeDataPath)
	if err != nil {
		return unlockStatusResponse{}, fmt.Errorf("list encrypted databases: %w", err)
	}
	for i := range databases {
		if runtime := s.workspaces[databases[i].ID]; runtime != nil && runtime.path == databases[i].Path {
			databases[i].Unlocked = true
		}
	}
	activeID := s.activeDatabase
	activeName := db.DefaultDatabaseName(s.config.DataPath)
	for _, item := range databases {
		if item.Path == s.activeDataPath {
			activeID = item.ID
			activeName = item.Name
			break
		}
		if item.ID == activeID {
			activeName = item.Name
		}
	}
	if s.database != nil {
		return unlockStatusResponse{State: "unlocked", DataPath: s.activeDataPath, DatabaseID: activeID, DatabaseName: activeName, DatabaseSizeBytes: fileSize(s.activeDataPath), UISessionAuthenticated: true, Databases: databases}, nil
	}
	if len(databases) == 0 {
		return unlockStatusResponse{State: "setup_required", DataPath: s.activeDataPath, DatabaseID: activeID, DatabaseName: activeName, Databases: databases}, nil
	}
	selected := databases[0]
	for _, item := range databases {
		if item.ID == activeID {
			selected = item
			break
		}
	}
	return unlockStatusResponse{
		State:        selected.State,
		DataPath:     selected.Path,
		DatabaseID:   selected.ID,
		DatabaseName: selected.Name,
		Databases:    databases,
	}, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (s *Server) openUnlockedLocked(password string) error {
	runtime, err := s.openRuntimeForLifecycle(s.activeDataPath, s.activeDatabase, password)
	if err != nil {
		return err
	}
	s.config.GatewaySecret = runtime.gatewaySecret
	s.workspaces[runtime.id] = runtime
	s.applyRuntimeLocked(runtime)
	s.initializeRetention(runtime)
	return nil
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
	return db.MoveDatabase(currentPath, targetPath)
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
	gatewaySecret, err := gatewaySecretFromDatabase(database, s.config.GatewaySecret)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	secretVault, err := vault.New(gatewaySecret)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	bindingRequired, err := recordcrypto.EnvelopeMarkerPresent(context.Background(), database)
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
	actionIdentityKey, err := deriveConnectorActionIdentityKey(gatewaySecret, workspaceUUID)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	runtimeInstanceID, err := executionprincipal.NewRuntimeInstanceID()
	if err != nil {
		clearBytes(actionIdentityKey)
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
		connectorResources: connectorRuntimeResources(s.connectorRegistry(), s.connectorAdapterRegistry(), database, secretVault, workspaceUUID),
		fileTransfers:      filetransfer.NewStore(database),
		transferCancels:    map[int64]context.CancelFunc{},
		credBoundaries:     map[int64]connectorCredentialBoundary{},
		batchCancels:       map[int64]context.CancelFunc{},
		transferControls:   map[int64]*transferControl{},
		batchControls:      map[int64]*transferControl{},
		workspaceUUID:      workspaceUUID,
		uiRetryIdentity:    uiRetryIdentity,
		runtimeInstanceID:  runtimeInstanceID,
		actionIdentityKey:  actionIdentityKey,
		vaultLeases:        vaultsessions.NewStore(),
	}
	if err := s.reconcileConnectorRuntimeSurfaces(context.Background(), runtime); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("reconcile connector runtime surfaces: %w", err)
	}
	settings, err := readSecuritySettingsFromDB(context.Background(), runtime)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	runtime.mcpStarted = settings.MCPStartEnabled
	runtime.securitySettings = settings
	runtime.securityLoaded = true
	runtime.consoleSessions = console.NewManager(database, s.runtimeConsoleOpener(runtime), s.runtimeRedactor(runtime))
	s.configureVaultSessionRuntime(runtime)
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

func gatewaySecretFromDatabase(database *sql.DB, fallback string) (string, error) {
	var stored string
	err := database.QueryRow(`SELECT value FROM settings WHERE key = 'gateway_secret'`).Scan(&stored)
	if err == nil && strings.TrimSpace(stored) != "" {
		return strings.TrimSpace(stored), nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("read gateway secret setting: %w", err)
	}
	bindingRequired, markerErr := recordcrypto.EnvelopeMarkerPresent(context.Background(), database)
	if markerErr != nil {
		return "", markerErr
	}
	if bindingRequired {
		return "", fmt.Errorf("gateway secret is missing from an envelope-bound database")
	}
	if strings.TrimSpace(fallback) == "" {
		return "", fmt.Errorf("gateway secret is missing")
	}
	_, err = database.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('gateway_secret', ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		strings.TrimSpace(fallback),
	)
	if err != nil {
		return "", fmt.Errorf("write gateway secret setting: %w", err)
	}
	return strings.TrimSpace(fallback), nil
}

func workspaceUUIDFromDatabase(ctx context.Context, database *sql.DB, bindingRequired bool) (string, error) {
	if bindingRequired {
		return projectvault.ReadWorkspaceUUID(ctx, database)
	}
	return projectvault.EnsureWorkspaceUUID(ctx, database)
}

func (s *Server) currentDataPath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeDataPath
}

func (s *Server) setupTargetPathLocked(databaseID string, databaseName string) (string, string, error) {
	databaseID = strings.TrimSpace(databaseID)
	databaseName = strings.TrimSpace(databaseName)
	if databaseName != "" {
		id, path, err := db.NewDatabasePath(s.config.DataPath, databaseName)
		return path, id, err
	}
	path, err := db.DatabasePath(s.config.DataPath, databaseID)
	if err != nil {
		return "", "", err
	}
	if databaseID == "" {
		databaseID = db.DefaultDatabaseID(s.config.DataPath)
	}
	return path, databaseID, nil
}

func (s *Server) unlockTargetPathLocked(databaseID string) (string, string, error) {
	databaseID = strings.TrimSpace(databaseID)
	if databaseID == "" {
		databaseID = s.activeDatabase
	}
	path, err := db.DatabasePath(s.config.DataPath, databaseID)
	if err != nil {
		return "", "", err
	}
	if databaseID == "" {
		databaseID = db.DefaultDatabaseID(s.config.DataPath)
	}
	return path, databaseID, nil
}

func (s *Server) closeActiveRuntimeLocked(promote bool) error {
	activeID := s.activeDatabase
	var closeErr error
	if activeID != "" {
		if runtime := s.workspaces[activeID]; runtime != nil {
			closeErr = s.closeRuntime(runtime)
		}
		delete(s.workspaces, activeID)
	}
	s.database = nil
	s.vault = nil
	s.tokens = nil
	if promote {
		for _, runtime := range s.workspaces {
			if runtime != nil {
				s.applyRuntimeLocked(runtime)
				return closeErr
			}
		}
	}
	return closeErr
}

func (s *Server) closeUnlockedResources() error {
	return s.closeActiveRuntimeLocked(false)
}

func (s *Server) closeAllUnlockedResources() error {
	seen := map[*databaseRuntime]bool{}
	var closeErrors []error
	for _, runtime := range s.workspaces {
		if runtime == nil || seen[runtime] {
			continue
		}
		seen[runtime] = true
		if err := s.closeRuntime(runtime); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	s.workspaces = map[string]*databaseRuntime{}
	s.database = nil
	s.vault = nil
	s.tokens = nil
	return errors.Join(closeErrors...)
}

func (s *Server) closeRuntimeByIDLocked(id string) error {
	runtime := s.workspaces[id]
	if runtime == nil {
		return nil
	}
	closeErr := s.closeRuntime(runtime)
	delete(s.workspaces, id)
	if s.activeDatabase == id {
		s.database = nil
		s.vault = nil
		s.tokens = nil
	}
	return closeErr
}

func (s *Server) unlockedRuntimeSnapshot() []*databaseRuntime {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runtimes := []*databaseRuntime{}
	seen := map[*databaseRuntime]bool{}
	if runtime := s.workspaces[s.activeDatabase]; runtime != nil {
		runtimes = append(runtimes, runtime)
		seen[runtime] = true
	}
	for _, runtime := range s.workspaces {
		if runtime == nil || seen[runtime] {
			continue
		}
		runtimes = append(runtimes, runtime)
		seen[runtime] = true
	}
	return runtimes
}

func (s *Server) activeRuntime() *databaseRuntime {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workspaces[s.activeDatabase]
}

func (s *Server) applyRuntimeLocked(runtime *databaseRuntime) {
	if runtime == nil {
		s.database = nil
		s.vault = nil
		s.tokens = nil
		return
	}
	s.activeDatabase = runtime.id
	s.activeDataPath = runtime.path
	if strings.TrimSpace(runtime.gatewaySecret) != "" {
		s.config.GatewaySecret = runtime.gatewaySecret
	}
	s.database = runtime.database
	s.vault = runtime.vault
	s.tokens = runtime.tokens
}

func (s *Server) closeRuntime(runtime *databaseRuntime) error {
	s.stopRetentionWorker(runtime)
	s.stopConnectorActionRecoveryWorker(runtime)
	if runtime.vaultLeases != nil {
		runtime.vaultLeases.Clear()
	}
	if runtime.consoleSessions != nil {
		runtime.consoleSessions.CloseAll()
	}
	if err := s.cancelRunningCommandRequests(context.Background(), runtime, "workspace locked while command was running"); err != nil {
		log.Printf("mark running command requests failed workspace=%s error=%v", runtime.id, err)
	}
	if err := s.markRunningConnectorActionsOutcomeUnknown(runtime); err != nil {
		log.Printf("mark running connector actions outcome unknown failed workspace=%s error=%v", runtime.id, err)
	}
	if runtime.fileTransfers != nil {
		runtime.cancelAllFileTransfers()
		if err := runtime.fileTransfers.FailActive(context.Background(), "workspace locked while file transfer was running", "workspace locked while file transfer queue was running"); err != nil {
			log.Printf("mark running file transfers failed workspace=%s error=%v", runtime.id, err)
		}
	}
	if runtime.auditDispatcher != nil {
		runtime.auditDispatcher.Stop()
	}
	clearBytes(runtime.actionIdentityKey)
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
	store := connectortargets.NewStore(runtime.database)
	for {
		requests, err := store.ListActionRequests(context.Background(), connectortargets.ActionRequestFilter{
			Status: string(connectors.ResultRunning),
			Limit:  100,
		})
		if err != nil {
			return err
		}
		if len(requests) == 0 {
			return nil
		}
		for _, request := range requests {
			if _, err := s.finishConnectorActionRequest(
				context.Background(), runtime, request.ID, connectors.ResultOutcomeUnknown,
				nil, "", db.ConnectorActionOutcomeUnknownMessage,
			); err != nil {
				return err
			}
		}
	}
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
