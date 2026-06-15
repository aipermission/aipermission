package api

import (
	"net/http"
	"strings"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

type renameDatabaseRequest struct {
	DatabaseName string `json:"database_name"`
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
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.DatabaseName = strings.TrimSpace(request.DatabaseName)
	if request.DatabaseName == "" {
		writeError(w, http.StatusBadRequest, "database name is required")
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
	_, _ = s.database.ExecContext(r.Context(), `PRAGMA wal_checkpoint(FULL)`)
	s.closeUnlockedResources()
	s.clearUISessions(w)

	if err := dbpkg.MoveDatabase(oldPath, path); err != nil {
		s.activeDataPath = oldPath
		writeInternalError(w)
		return
	}
	s.activeDataPath = path
	s.activeDatabase = id

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "renamed",
		"state":       "locked",
		"database_id": id,
		"renamed_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s databaseHandlers) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	var request deleteDatabaseRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.CurrentPassword)
	request.ConfirmName = strings.TrimSpace(request.ConfirmName)
	if request.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "current password is required")
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
		writeError(w, http.StatusUnauthorized, "invalid current database password")
		return
	}

	path := s.activeDataPath
	_, _ = s.database.ExecContext(r.Context(), `PRAGMA wal_checkpoint(FULL)`)
	s.closeActiveRuntimeLocked(true)
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

func (s databaseHandlers) deleteLockedDatabase(w http.ResponseWriter, r *http.Request) {
	var request deleteLockedDatabaseRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
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
		writeError(w, http.StatusUnauthorized, "invalid database password")
		return
	}
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
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.Password)
	request.DatabaseID = strings.TrimSpace(request.DatabaseID)

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
		writeDatabaseUnlockError(w, err)
		return
	}

	s.config.GatewaySecret = runtime.gatewaySecret
	s.workspaces[targetID] = runtime
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
}

func (s databaseHandlers) changeDatabasePassword(w http.ResponseWriter, r *http.Request) {
	var request changeDatabasePasswordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
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

	s.mu.Lock()
	defer s.mu.Unlock()
	runtime := s.workspaces[s.activeDatabase]
	if runtime == nil || runtime.database == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}

	if err := dbpkg.ValidateEncrypted(runtime.path, request.CurrentPassword); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid current database password")
		return
	}

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
