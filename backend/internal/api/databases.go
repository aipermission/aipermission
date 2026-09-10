package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
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

	runtime := s.activeRuntime()
	if runtime == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}

	oldPath := runtime.path
	id, path, err := databasecatalog.RenameDatabaseTarget(s.config.DataPath, oldPath, request.DatabaseName)
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
	if err := dbpkg.CheckpointForFilesystemMutation(r.Context(), runtime.database); err != nil {
		writeInternalError(w)
		return
	}
	if err := s.closeUnlockedResources(); err != nil {
		s.workspaces.Select(workspaceIdentity(runtime.id, oldPath))
		if reopenErr := s.openUnlockedLocked(request.CurrentPassword); reopenErr != nil {
			s.clearUISessions(w)
		}
		writeInternalError(w)
		return
	}

	if err := s.moveDatabase(oldPath, path); err != nil {
		s.workspaces.Select(workspaceIdentity(runtime.id, oldPath))
		if reopenErr := s.openUnlockedLocked(request.CurrentPassword); reopenErr != nil {
			s.clearUISessions(w)
		}
		writeInternalError(w)
		return
	}
	s.workspaces.Select(workspaceIdentity(id, path))
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

	runtime := s.activeRuntime()
	if runtime == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}

	expectedName := s.currentDatabaseNameLocked()
	if request.ConfirmName != expectedName {
		writeError(w, http.StatusBadRequest, "database name confirmation does not match")
		return
	}
	if runtime.database == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}
	if err := dbpkg.ValidateEncrypted(runtime.path, request.CurrentPassword); err != nil {
		attempt.failure()
		writeError(w, http.StatusUnauthorized, "invalid current database password")
		return
	}
	attempt.success()

	path := runtime.path
	if err := dbpkg.CheckpointForFilesystemMutation(r.Context(), runtime.database); err != nil {
		writeInternalError(w)
		return
	}
	if err := s.closeActiveRuntimeLocked(true); err != nil {
		writeInternalError(w)
		return
	}
	if err := databasecatalog.DeleteDatabase(path); err != nil {
		writeInternalError(w)
		return
	}
	if s.activeRuntime() == nil {
		s.workspaces.ResetSelection(databasecatalog.DefaultDatabaseID(s.config.DataPath))
	}
	state := "locked"
	if s.activeRuntime() != nil {
		state = "unlocked"
		if err := s.issueUISessionLocked(w); err != nil {
			writeInternalError(w)
			return
		}
	} else {
		s.clearUISessions(w)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "deleted",
		"state":       state,
		"database_id": s.workspaces.Selection().ID,
		"deleted_at":  time.Now().UTC().Format(time.RFC3339),
	})
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

	targetPath, targetID, err := s.unlockTargetPathLocked(request.DatabaseID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if runtime, exists := s.workspaces.Lookup(targetID); exists && runtime != nil {
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
	if err := databasecatalog.DeleteDatabase(targetPath); err != nil {
		writeInternalError(w)
		return
	}
	if s.workspaces.Selection().ID == targetID {
		s.workspaces.ResetSelection(databasecatalog.DefaultDatabaseID(s.config.DataPath))
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

	transition, err := s.workspaceLifecycle.Switch(request.DatabaseID, request.Password)
	if err != nil {
		if request.Password != "" {
			recordDatabaseUnlockAttempt(attempt, err)
		}
		writeWorkspaceLifecycleError(w, err)
		return
	}
	if request.Password != "" {
		attempt.success()
	}
	if err := s.issueUISessionLocked(w); err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      transition.Status,
		"state":       "unlocked",
		"database_id": transition.Identity.ID,
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

	runtime := s.activeRuntime()
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
		if err := backups.ValidateRemoteBackupPassword(request.NewPassword, s.currentDatabaseNameLocked()); err != nil {
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

	_ = dbpkg.CheckpointFull(r.Context(), runtime.database)
	if err := dbpkg.Rekey(runtime.database, request.NewPassword); err != nil {
		writeInternalError(w)
		return
	}
	_ = dbpkg.CheckpointFull(r.Context(), runtime.database)

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
	selection := s.workspaces.Selection()
	items, err := databasecatalog.ListDatabases(s.config.DataPath, selection.Path)
	if err != nil {
		return selection.ID
	}
	for _, item := range items {
		if item.Path == selection.Path || item.ID == selection.ID {
			return item.Name
		}
	}
	return selection.ID
}
