package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

type renameDatabaseRequest struct {
	DatabaseName    string `json:"database_name"`
	CurrentPassword string `json:"current_password"`
}

type deleteDatabaseRequest struct {
	ConfirmName     string `json:"confirm_name"`
	CurrentPassword string `json:"current_password"`
}

type deleteLockedDatabaseRequest struct {
	DatabaseID      string `json:"database_id"`
	CurrentPassword string `json:"current_password"`
}

type switchDatabaseRequest struct {
	DatabaseID string `json:"database_id"`
	Password   string `json:"password"`
}

type changeDatabasePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (s databaseHandlers) renameDatabase(w http.ResponseWriter, r *http.Request) {
	var request renameDatabaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defer clearStringReferences(&request.CurrentPassword)
	request.DatabaseName = strings.TrimSpace(request.DatabaseName)
	if request.DatabaseName == "" {
		writeError(w, http.StatusBadRequest, "database name is required")
		return
	}
	if request.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "current password is required")
		return
	}
	attempt, ok := s.beginDatabasePasswordAttempt(w, r)
	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.database == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}

	oldPath := s.activeDataPath
	id, path, err := dbpkg.RenameDatabaseTarget(s.config.DataPath, oldPath, request.DatabaseName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := dbpkg.ValidateEncrypted(oldPath, request.CurrentPassword); err != nil {
		attempt.failure()
		writeError(w, http.StatusUnauthorized, "invalid current database password")
		return
	}
	attempt.success()
	if err := checkpointDatabaseForMove(r.Context(), s.database); err != nil {
		writeInternalError(w)
		return
	}
	if err := s.closeUnlockedResources(); err != nil {
		s.activeDataPath = oldPath
		if reopenErr := s.openUnlockedLocked(request.CurrentPassword); reopenErr != nil {
			s.clearUISessions(w)
		}
		writeInternalError(w)
		return
	}

	if err := s.moveDatabase(oldPath, path); err != nil {
		s.activeDataPath = oldPath
		if reopenErr := s.openUnlockedLocked(request.CurrentPassword); reopenErr != nil {
			s.clearUISessions(w)
		}
		writeInternalError(w)
		return
	}
	s.activeDataPath = path
	s.activeDatabase = id
	s.clearUISessions(w)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "renamed",
		"state":       "locked",
		"database_id": id,
		"renamed_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s databaseHandlers) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	var request deleteDatabaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defer clearStringReferences(&request.CurrentPassword)
	request.ConfirmName = strings.TrimSpace(request.ConfirmName)
	if request.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "current password is required")
		return
	}
	attempt, ok := s.beginDatabasePasswordAttempt(w, r)
	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.database == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}

	expectedName := s.currentDatabaseNameLocked()
	if request.ConfirmName != expectedName {
		writeError(w, http.StatusBadRequest, "database name confirmation does not match")
		return
	}
	runtime := s.workspaces[s.activeDatabase]
	if runtime == nil || runtime.database == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}
	if err := dbpkg.ValidateEncrypted(runtime.path, request.CurrentPassword); err != nil {
		attempt.failure()
		writeError(w, http.StatusUnauthorized, "invalid current database password")
		return
	}
	attempt.success()

	path := s.activeDataPath
	if err := checkpointDatabaseForMove(r.Context(), s.database); err != nil {
		writeInternalError(w)
		return
	}
	if err := s.closeActiveRuntimeLocked(true); err != nil {
		writeInternalError(w)
		return
	}
	if err := dbpkg.DeleteDatabase(path); err != nil {
		writeInternalError(w)
		return
	}
	if s.database == nil {
		s.activeDataPath = s.config.DataPath
		s.activeDatabase = dbpkg.DefaultDatabaseID(s.config.DataPath)
	}
	state := "locked"
	if s.database != nil {
		state = "unlocked"
		if err := s.issueUISession(w); err != nil {
			writeInternalError(w)
			return
		}
	} else {
		s.clearUISessions(w)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "deleted",
		"state":       state,
		"database_id": s.activeDatabase,
		"deleted_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

func checkpointDatabaseForMove(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("database is not open")
	}
	var busy, logFrames, checkpointedFrames int
	if err := database.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return fmt.Errorf("checkpoint database before filesystem mutation: %w", err)
	}
	if busy != 0 || checkpointedFrames < logFrames {
		return fmt.Errorf("checkpoint database before filesystem mutation: database remained busy")
	}
	return nil
}

func (s databaseHandlers) deleteLockedDatabase(w http.ResponseWriter, r *http.Request) {
	var request deleteLockedDatabaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defer clearStringReferences(&request.CurrentPassword)
	request.DatabaseID = strings.TrimSpace(request.DatabaseID)
	if request.DatabaseID == "" {
		writeError(w, http.StatusBadRequest, "database id is required")
		return
	}
	if request.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "database password is required")
		return
	}
	attempt, ok := s.beginDatabasePasswordAttempt(w, r)
	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	targetPath, targetID, err := s.unlockTargetPathLocked(request.DatabaseID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if runtime := s.workspaces[targetID]; runtime != nil {
		writeError(w, http.StatusConflict, "database is currently unlocked; lock it before deleting from the unlock screen")
		return
	}
	if !dbpkg.Exists(targetPath) {
		writeError(w, http.StatusNotFound, "encrypted database is not initialized")
		return
	}
	if dbpkg.LooksLikePlainSQLite(targetPath) {
		writeError(w, http.StatusConflict, "plaintext SQLite databases are not supported; remove this file manually")
		return
	}
	if err := dbpkg.ValidateEncrypted(targetPath, request.CurrentPassword); err != nil {
		attempt.failure()
		writeError(w, http.StatusUnauthorized, "invalid database password")
		return
	}
	attempt.success()
	if err := dbpkg.DeleteDatabase(targetPath); err != nil {
		writeInternalError(w)
		return
	}
	if s.activeDatabase == targetID {
		s.activeDataPath = s.config.DataPath
		s.activeDatabase = dbpkg.DefaultDatabaseID(s.config.DataPath)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "deleted",
		"state":       "locked",
		"database_id": targetID,
		"deleted_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s databaseHandlers) switchDatabase(w http.ResponseWriter, r *http.Request) {
	var request switchDatabaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defer clearStringReferences(&request.Password)
	request.DatabaseID = strings.TrimSpace(request.DatabaseID)
	var attempt databasePasswordAttempt
	if request.Password != "" {
		var ok bool
		attempt, ok = s.beginDatabasePasswordAttempt(w, r)
		if !ok {
			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.workspaces) == 0 {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}

	targetPath, targetID, err := s.unlockTargetPathLocked(request.DatabaseID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.database != nil && (targetID == s.activeDatabase || targetPath == s.activeDataPath) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "current",
			"state":       "unlocked",
			"database_id": s.activeDatabase,
		})
		return
	}
	if runtime := s.workspaces[targetID]; runtime != nil && runtime.path == targetPath {
		s.applyRuntimeLocked(runtime)
		if err := s.issueUISession(w); err != nil {
			writeInternalError(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "switched",
			"state":       "unlocked",
			"database_id": targetID,
			"switched_at": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	if request.Password == "" {
		writeError(w, http.StatusBadRequest, "database password is required")
		return
	}
	if !dbpkg.Exists(targetPath) {
		writeError(w, http.StatusNotFound, "encrypted database is not initialized")
		return
	}
	if dbpkg.LooksLikePlainSQLite(targetPath) {
		writeError(w, http.StatusConflict, "plaintext SQLite databases are not supported; create or import an encrypted .aipdb database")
		return
	}

	runtime, err := s.openRuntime(targetPath, targetID, request.Password)
	if err != nil {
		recordDatabaseUnlockAttempt(attempt, err)
		writeDatabaseUnlockError(w, err)
		return
	}
	attempt.success()

	s.config.GatewaySecret = runtime.gatewaySecret
	s.workspaces[targetID] = runtime
	s.applyRuntimeLocked(runtime)
	s.initializeRetention(runtime)
	if err := s.issueUISession(w); err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "switched",
		"state":       "unlocked",
		"database_id": targetID,
		"switched_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s databaseHandlers) changeDatabasePassword(w http.ResponseWriter, r *http.Request) {
	var request changeDatabasePasswordRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defer clearStringReferences(&request.CurrentPassword, &request.NewPassword, &request.ConfirmPassword)
	if request.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "current password is required")
		return
	}
	if err := validateUnlockPassword(request.NewPassword, request.ConfirmPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.CurrentPassword == request.NewPassword {
		writeError(w, http.StatusBadRequest, "new password must be different from the current password")
		return
	}
	attempt, ok := s.beginDatabasePasswordAttempt(w, r)
	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	runtime := s.workspaces[s.activeDatabase]
	if runtime == nil || runtime.database == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}
	backupStore := backups.NewStore(runtime.database)
	hasActiveRemoteBackup, err := backupStore.HasActiveProvider(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	if hasActiveRemoteBackup {
		if err := validateRemoteBackupPassword(request.NewPassword, s.currentDatabaseNameLocked()); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if err := dbpkg.ValidateEncrypted(runtime.path, request.CurrentPassword); err != nil {
		attempt.failure()
		writeError(w, http.StatusUnauthorized, "invalid current database password")
		return
	}
	attempt.success()

	_, _ = runtime.database.ExecContext(r.Context(), `PRAGMA wal_checkpoint(FULL)`)
	if err := dbpkg.Rekey(runtime.database, request.NewPassword); err != nil {
		writeInternalError(w)
		return
	}
	_, _ = runtime.database.ExecContext(r.Context(), `PRAGMA wal_checkpoint(FULL)`)

	if err := dbpkg.ValidateEncrypted(runtime.path, request.NewPassword); err != nil {
		writeError(w, http.StatusInternalServerError, "database password changed but verification reopen failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "password_changed",
		"state":      "unlocked",
		"changed_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) currentDatabaseNameLocked() string {
	items, err := dbpkg.ListDatabases(s.config.DataPath, s.activeDataPath)
	if err != nil {
		return s.activeDatabase
	}
	for _, item := range items {
		if item.Path == s.activeDataPath || item.ID == s.activeDatabase {
			return item.Name
		}
	}
	return s.activeDatabase
}
