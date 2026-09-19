package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

func (h *Handlers) Rename(w http.ResponseWriter, r *http.Request) {
	var request RenameRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.CurrentPassword)
	request.DatabaseName = strings.TrimSpace(request.DatabaseName)
	if request.DatabaseName == "" || request.CurrentPassword == "" {
		message := "database name is required"
		if request.DatabaseName != "" {
			message = "current password is required"
		}
		httptransport.WriteError(w, http.StatusBadRequest, message)
		return
	}
	attempt, ok := h.beginAttempt(w, r)
	if !ok {
		return
	}
	transition, err := h.dependencies.Lifecycle.Rename(r.Context(), request.DatabaseName, request.CurrentPassword)
	if !h.handleMutationResult(w, attempt, err, "invalid current database password") {
		if !h.dependencies.Lifecycle.IsUnlocked() && h.dependencies.ClearSessions != nil {
			h.dependencies.ClearSessions(w)
		}
		return
	}
	if h.dependencies.ClearSessions != nil {
		h.dependencies.ClearSessions(w)
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "renamed", "state": "locked", "database_id": transition.Identity.ID,
		"renamed_at": h.now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	var request DeleteRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.CurrentPassword)
	if request.CurrentPassword == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "current password is required")
		return
	}
	attempt, ok := h.beginAttempt(w, r)
	if !ok {
		return
	}
	transition, err := h.dependencies.Lifecycle.DeleteCurrent(r.Context(), strings.TrimSpace(request.ConfirmName), request.CurrentPassword)
	if !h.handleMutationResult(w, attempt, err, "invalid current database password") {
		return
	}
	if transition.State == "unlocked" {
		if !h.issueSession(w) {
			return
		}
	} else if h.dependencies.ClearSessions != nil {
		h.dependencies.ClearSessions(w)
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "deleted", "state": transition.State, "database_id": transition.Identity.ID,
		"deleted_at": h.now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) DeleteLocked(w http.ResponseWriter, r *http.Request) {
	var request DeleteLockedRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.CurrentPassword)
	request.DatabaseID = strings.TrimSpace(request.DatabaseID)
	if request.DatabaseID == "" || request.CurrentPassword == "" {
		message := "database id is required"
		if request.DatabaseID != "" {
			message = "database password is required"
		}
		httptransport.WriteError(w, http.StatusBadRequest, message)
		return
	}
	attempt, ok := h.beginAttempt(w, r)
	if !ok {
		return
	}
	transition, err := h.dependencies.Lifecycle.DeleteLocked(request.DatabaseID, request.CurrentPassword)
	if !h.handleMutationResult(w, attempt, err, "invalid database password") {
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "deleted", "state": "locked", "database_id": transition.Identity.ID,
		"deleted_at": h.now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) Switch(w http.ResponseWriter, r *http.Request) {
	var request SwitchRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.Password)
	request.DatabaseID = strings.TrimSpace(request.DatabaseID)
	var attempt PasswordAttempt
	if request.Password != "" {
		var ok bool
		attempt, ok = h.beginAttempt(w, r)
		if !ok {
			return
		}
	}
	transition, err := h.dependencies.Lifecycle.Switch(r.Context(), request.DatabaseID, request.Password)
	if err != nil {
		if attempt != nil {
			if errors.Is(err, workspacelifecycle.ErrAuthentication) {
				attempt.Failure()
			} else {
				attempt.Success()
			}
		}
		writeUnlockError(w, err)
		return
	}
	if attempt != nil {
		attempt.Success()
	}
	if !h.issueSession(w) {
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status": transition.Status, "state": "unlocked", "database_id": transition.Identity.ID,
		"switched_at": h.now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var request ChangePasswordRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.CurrentPassword, &request.NewPassword, &request.ConfirmPassword)
	if request.CurrentPassword == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "current password is required")
		return
	}
	if err := workspacelifecycle.ValidatePassword(request.NewPassword, request.ConfirmPassword); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.CurrentPassword == request.NewPassword {
		httptransport.WriteError(w, http.StatusBadRequest, "new password must be different from the current password")
		return
	}
	attempt, ok := h.beginAttempt(w, r)
	if !ok {
		return
	}
	err := h.dependencies.Lifecycle.ChangePassword(r.Context(), request.CurrentPassword, request.NewPassword)
	if databaseID, invalidate := workspacelifecycle.SessionInvalidationDatabase(err); invalidate {
		if h.dependencies.InvalidateSessions != nil {
			h.dependencies.InvalidateSessions(databaseID)
		}
		if h.dependencies.ExpireSession != nil {
			h.dependencies.ExpireSession(w)
		}
	}
	if !h.handleMutationResult(w, attempt, err, "invalid current database password") {
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "password_changed", "state": "unlocked",
		"changed_at": h.now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) handleMutationResult(w http.ResponseWriter, attempt PasswordAttempt, err error, credentialMessage string) bool {
	if err == nil {
		attempt.Success()
		return true
	}
	if errors.Is(err, workspacelifecycle.ErrCredential) {
		attempt.Failure()
		httptransport.WriteError(w, http.StatusUnauthorized, credentialMessage)
		return false
	}
	if workspacelifecycle.CredentialWasVerified(err) {
		attempt.Success()
	}
	writeMutationError(w, err)
	return false
}

func writeMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspacelifecycle.ErrLocked):
		httptransport.WriteError(w, http.StatusLocked, err.Error())
	case errors.Is(err, workspacelifecycle.ErrNameRequired), errors.Is(err, workspacelifecycle.ErrNameConfirmation),
		errors.Is(err, workspacelifecycle.ErrPasswordRequired), errors.Is(err, workspacelifecycle.ErrInvalidRequest),
		errors.Is(err, workspacelifecycle.ErrPasswordPolicy):
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, workspacelifecycle.ErrNotInitialized):
		httptransport.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, workspacelifecycle.ErrPlaintext), errors.Is(err, workspacelifecycle.ErrRuntimeUnlocked):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}
