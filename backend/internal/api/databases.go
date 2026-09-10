package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
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

	transition, err := s.workspaceLifecycle.Rename(r.Context(), request.DatabaseName, request.CurrentPassword)
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrCredential) {
			attempt.failure()
			writeError(w, http.StatusUnauthorized, "invalid current database password")
			return
		}
		if workspacelifecycle.CredentialWasVerified(err) {
			attempt.success()
		}
		if s.activeRuntime() == nil {
			s.clearUISessions(w)
		}
		writeWorkspaceMutationError(w, err)
		return
	}
	attempt.success()
	s.clearUISessions(w)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "renamed",
		"state":       "locked",
		"database_id": transition.Identity.ID,
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

	transition, err := s.workspaceLifecycle.DeleteCurrent(r.Context(), request.ConfirmName, request.CurrentPassword)
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrCredential) {
			attempt.failure()
			writeError(w, http.StatusUnauthorized, "invalid current database password")
			return
		}
		if workspacelifecycle.CredentialWasVerified(err) {
			attempt.success()
		}
		writeWorkspaceMutationError(w, err)
		return
	}
	attempt.success()
	if transition.State == "unlocked" {
		if err := s.issueUISessionLocked(w); err != nil {
			writeInternalError(w)
			return
		}
	} else {
		s.clearUISessions(w)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "deleted",
		"state":       transition.State,
		"database_id": transition.Identity.ID,
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

	transition, err := s.workspaceLifecycle.DeleteLocked(request.DatabaseID, request.CurrentPassword)
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrCredential) {
			attempt.failure()
			writeError(w, http.StatusUnauthorized, "invalid database password")
			return
		}
		if workspacelifecycle.CredentialWasVerified(err) {
			attempt.success()
		}
		writeWorkspaceMutationError(w, err)
		return
	}
	attempt.success()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "deleted",
		"state":       "locked",
		"database_id": transition.Identity.ID,
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

	if err := s.workspaceLifecycle.ChangePassword(r.Context(), request.CurrentPassword, request.NewPassword); err != nil {
		if errors.Is(err, workspacelifecycle.ErrCredential) {
			attempt.failure()
			writeError(w, http.StatusUnauthorized, "invalid current database password")
			return
		}
		if workspacelifecycle.CredentialWasVerified(err) {
			attempt.success()
		}
		writeWorkspaceMutationError(w, err)
		return
	}
	attempt.success()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "password_changed",
		"state":      "unlocked",
		"changed_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func writeWorkspaceMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspacelifecycle.ErrLocked):
		writeError(w, http.StatusLocked, err.Error())
	case errors.Is(err, workspacelifecycle.ErrNameRequired), errors.Is(err, workspacelifecycle.ErrNameConfirmation),
		errors.Is(err, workspacelifecycle.ErrPasswordRequired), errors.Is(err, workspacelifecycle.ErrInvalidRequest),
		errors.Is(err, workspacelifecycle.ErrPasswordPolicy):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, workspacelifecycle.ErrNotInitialized):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, workspacelifecycle.ErrPlaintext), errors.Is(err, workspacelifecycle.ErrRuntimeUnlocked):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeInternalError(w)
	}
}

func (s *Server) currentDatabaseNameLocked() string {
	status, err := s.workspaceLifecycle.Status()
	if err == nil && status.DatabaseName != "" {
		return status.DatabaseName
	}
	return s.workspaceSelection().ID
}
