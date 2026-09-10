package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type unlockRequest struct {
	Password   string `json:"password"`
	DatabaseID string `json:"database_id"`
}

type setupUnlockRequest struct {
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	DatabaseID      string `json:"database_id"`
	DatabaseName    string `json:"database_name"`
}

type unlockStatusResponse struct {
	State                  string                         `json:"state"`
	DataPath               string                         `json:"data_path,omitempty"`
	DatabaseID             string                         `json:"database_id"`
	DatabaseName           string                         `json:"database_name"`
	DatabaseSizeBytes      int64                          `json:"database_size_bytes,omitempty"`
	UISessionAuthenticated bool                           `json:"ui_session_authenticated"`
	Databases              []databasecatalog.DatabaseInfo `json:"databases"`
}

func (s unlockHandlers) unlockStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.currentUnlockStatus()
	if err != nil {
		writeInternalError(w)
		return
	}
	if status.State == "unlocked" {
		status.UISessionAuthenticated = s.hasValidUISession(r)
		if !status.UISessionAuthenticated {
			status.State = "session_required"
		}
	}
	status = status.withoutLocalPaths()
	writeJSON(w, http.StatusOK, status)
}

func (status unlockStatusResponse) withoutLocalPaths() unlockStatusResponse {
	status.DataPath = ""
	for i := range status.Databases {
		status.Databases[i].Path = ""
	}
	return status
}

func writeDatabaseUnlockError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrDatabaseInUse) {
		writeError(w, http.StatusConflict, db.ErrDatabaseInUse.Error())
		return
	}
	if errors.Is(err, errDatabaseAuthentication) {
		writeError(w, http.StatusUnauthorized, "invalid unlock password or database")
		return
	}
	if message := db.UnsupportedSchemaMessage(err); message != "" {
		writeError(w, http.StatusConflict, message)
		return
	}
	if errors.Is(err, errDatabaseInitialization) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "database runtime initialization failed")
}

func recordDatabaseUnlockAttempt(attempt databasePasswordAttempt, err error) {
	if errors.Is(err, errDatabaseAuthentication) {
		attempt.failure()
		return
	}
	attempt.success()
}

func (s unlockHandlers) setupUnlock(w http.ResponseWriter, r *http.Request) {
	var request setupUnlockRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defer clearStringReferences(&request.Password, &request.ConfirmPassword)
	if err := validateUnlockPassword(request.Password, request.ConfirmPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	transition, err := s.workspaceLifecycle.Setup(request.DatabaseID, request.DatabaseName, request.Password)
	if err != nil {
		writeWorkspaceLifecycleError(w, err)
		return
	}
	if transition.Status == "current" {
		status, err := s.currentUnlockStatusLocked()
		if err != nil {
			writeInternalError(w)
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

	if err := s.issueUISessionLocked(w); err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "unlocked",
		"state":       "unlocked",
		"unlocked_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s unlockHandlers) unlock(w http.ResponseWriter, r *http.Request) {
	var request unlockRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defer clearStringReferences(&request.Password)
	attempt, ok := s.beginDatabasePasswordAttempt(w, r)
	if !ok {
		return
	}

	transition, err := s.workspaceLifecycle.Unlock(request.DatabaseID, request.Password)
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrCredential) {
			attempt.failure()
			writeError(w, http.StatusUnauthorized, "invalid unlock password or database")
			return
		}
		recordDatabaseUnlockAttempt(attempt, err)
		writeWorkspaceLifecycleError(w, err)
		return
	}
	attempt.success()
	if err := s.issueUISessionLocked(w); err != nil {
		writeInternalError(w)
		return
	}

	if transition.Opened {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "unlocked", "state": "unlocked", "unlocked_at": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	status, err := s.currentUnlockStatusLocked()
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s unlockHandlers) lock(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Scope string `json:"scope"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if !decodeJSON(w, r, &request) {
			return
		}
	}
	request.Scope = strings.TrimSpace(request.Scope)
	if request.Scope == "" {
		request.Scope = "current"
	}
	if s.workspaceLifecycle.WillLockAll(request.Scope) {
		s.closeMaintenanceConsoleForLifecycle("database_lock_" + request.Scope)
	}
	status, err := s.workspaceLifecycle.Lock(request.Scope)
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrInvalidScope) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeInternalError(w)
		}
		return
	}
	if request.Scope == "all" {
		s.clearUISessions(w)
	} else {
		if status.State != "unlocked" {
			s.clearUISessions(w)
		} else if err := s.issueUISessionLocked(w); err != nil {
			writeInternalError(w)
			return
		}
	}
	writeJSON(w, http.StatusOK, unlockStatusFromLifecycle(status))
}

func writeWorkspaceLifecycleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspacelifecycle.ErrLocked):
		writeError(w, http.StatusLocked, err.Error())
	case errors.Is(err, workspacelifecycle.ErrNotInitialized):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, workspacelifecycle.ErrPasswordRequired):
		writeError(w, http.StatusBadRequest, "password is required")
	case errors.Is(err, workspacelifecycle.ErrPlaintext):
		writeError(w, http.StatusConflict, "plaintext SQLite databases are not supported; create or import an encrypted .aipdb database")
	case errors.Is(err, errDatabaseAuthentication), errors.Is(err, errDatabaseInitialization), errors.Is(err, db.ErrDatabaseInUse):
		writeDatabaseUnlockError(w, err)
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
