package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/servers"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func (s *Server) isUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.workspaces) > 0
}

func (s *Server) currentUnlockStatus() unlockStatusResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentUnlockStatusLocked()
}

func (s *Server) currentUnlockStatusLocked() unlockStatusResponse {
	databases, _ := db.ListDatabases(s.config.DataPath, s.activeDataPath)
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
		return unlockStatusResponse{State: "unlocked", DataPath: s.activeDataPath, DatabaseID: activeID, DatabaseName: activeName, UISessionAuthenticated: true, Databases: databases}
	}
	if len(databases) == 0 {
		return unlockStatusResponse{State: "setup_required", DataPath: s.activeDataPath, DatabaseID: activeID, DatabaseName: activeName, Databases: databases}
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
	}
}

func (s *Server) openUnlockedLocked(password string) error {
	runtime, err := s.openRuntime(s.activeDataPath, s.activeDatabase, password)
	if err != nil {
		return err
	}
	s.config.GatewaySecret = runtime.gatewaySecret
	s.workspaces[runtime.id] = runtime
	s.applyRuntimeLocked(runtime)
	if settings, err := readRetentionSettings(context.Background(), runtime); err == nil {
		if _, err := applyRetentionSettings(context.Background(), runtime, settings); err != nil {
			log.Printf("retention cleanup failed workspace=%s error=%v", runtime.id, err)
		}
	} else {
		log.Printf("read retention settings failed workspace=%s error=%v", runtime.id, err)
	}
	return nil
}

func (s *Server) openRuntime(path string, id string, password string) (*databaseRuntime, error) {
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
	runtime := &databaseRuntime{
		id:               id,
		path:             path,
		gatewaySecret:    gatewaySecret,
		database:         database,
		vault:            secretVault,
		servers:          servers.NewStore(database),
		sshKeys:          sshkeys.NewStore(database, secretVault),
		tokens:           tokens.NewStore(database, secretVault),
		fileTransfers:    filetransfer.NewStore(database),
		transferCancels:  map[int64]context.CancelFunc{},
		batchCancels:     map[int64]context.CancelFunc{},
		transferControls: map[int64]*transferControl{},
		batchControls:    map[int64]*transferControl{},
	}
	settings, err := readSecuritySettingsFromDB(context.Background(), runtime)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := runtime.servers.SyncConnectorTargets(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	runtime.mcpStarted = settings.MCPStartEnabled
	runtime.securitySettings = settings
	runtime.securityLoaded = true
	runtime.consoleSessions = console.NewManager(database, s.serverSSHMaterialForRuntime(runtime), s.knownHostsPath(), s.runtimeRedactor(runtime))
	return runtime, nil
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

func (s *Server) closeActiveRuntimeLocked(promote bool) {
	activeID := s.activeDatabase
	if activeID != "" {
		if runtime := s.workspaces[activeID]; runtime != nil {
			s.closeRuntime(runtime)
		}
		delete(s.workspaces, activeID)
	}
	s.database = nil
	s.vault = nil
	s.servers = nil
	s.sshKeys = nil
	s.tokens = nil
	if promote {
		for _, runtime := range s.workspaces {
			if runtime != nil {
				s.applyRuntimeLocked(runtime)
				return
			}
		}
	}
}

func (s *Server) closeUnlockedResources() {
	s.closeActiveRuntimeLocked(false)
}

func (s *Server) closeAllUnlockedResources() {
	seen := map[*databaseRuntime]bool{}
	for _, runtime := range s.workspaces {
		if runtime == nil || seen[runtime] {
			continue
		}
		seen[runtime] = true
		s.closeRuntime(runtime)
	}
	s.workspaces = map[string]*databaseRuntime{}
	s.database = nil
	s.vault = nil
	s.servers = nil
	s.sshKeys = nil
	s.tokens = nil
}

func (s *Server) closeRuntimeByIDLocked(id string) {
	runtime := s.workspaces[id]
	if runtime == nil {
		return
	}
	s.closeRuntime(runtime)
	delete(s.workspaces, id)
	if s.activeDatabase == id {
		s.database = nil
		s.vault = nil
		s.servers = nil
		s.sshKeys = nil
		s.tokens = nil
	}
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
		s.servers = nil
		s.sshKeys = nil
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
	s.servers = runtime.servers
	s.sshKeys = runtime.sshKeys
	s.tokens = runtime.tokens
}

func (s *Server) closeRuntime(runtime *databaseRuntime) {
	if runtime.consoleSessions != nil {
		runtime.consoleSessions.CloseAll()
	}
	if err := s.cancelRunningCommandRequests(context.Background(), runtime, "workspace locked while command was running"); err != nil {
		log.Printf("mark running command requests failed workspace=%s error=%v", runtime.id, err)
	}
	if runtime.fileTransfers != nil {
		runtime.cancelAllFileTransfers()
		if err := runtime.fileTransfers.FailActive(context.Background(), "workspace locked while file transfer was running", "workspace locked while file transfer queue was running"); err != nil {
			log.Printf("mark running file transfers failed workspace=%s error=%v", runtime.id, err)
		}
	}
	if runtime.database != nil {
		_ = runtime.database.Close()
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
	case "/health", "/api/status", "/api/unlock/status", "/api/unlock/setup", "/api/unlock", "/api/backup/import":
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
