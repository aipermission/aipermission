package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/db"
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
	State                  string            `json:"state"`
	DataPath               string            `json:"data_path,omitempty"`
	DatabaseID             string            `json:"database_id"`
	DatabaseName           string            `json:"database_name"`
	DatabaseSizeBytes      int64             `json:"database_size_bytes,omitempty"`
	UISessionAuthenticated bool              `json:"ui_session_authenticated"`
	Databases              []db.DatabaseInfo `json:"databases"`
}

func (s unlockHandlers) unlockStatus(w http.ResponseWriter, r *http.Request) {
	status := s.currentUnlockStatus()
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
	if message := db.UnsupportedSchemaMessage(err); message != "" {
		writeError(w, http.StatusConflict, message)
		return
	}
	writeError(w, http.StatusUnauthorized, "invalid unlock password or database")
}

func (s unlockHandlers) setupUnlock(w http.ResponseWriter, r *http.Request) {
	var request setupUnlockRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.Password, &request.ConfirmPassword)
	if err := validateUnlockPassword(request.Password, request.ConfirmPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.database != nil {
		writeJSON(w, http.StatusOK, s.currentUnlockStatusLocked())
		return
	}

	targetPath, targetID, err := s.setupTargetPathLocked(request.DatabaseID, request.DatabaseName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.activeDataPath = targetPath
	s.activeDatabase = targetID

	if db.Exists(targetPath) {
		if rejectPlaintextDatabase(w, targetPath) {
			return
		}
		writeError(w, http.StatusConflict, "encrypted database already exists; unlock it or create a new database")
		return
	}

	if err := s.openUnlockedLocked(request.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.issueUISession(w); err != nil {
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
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.Password)
	if request.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}
	limitKey := authRateLimitKey(r, "unlock")
	if err := s.authLimiter.wait(r.Context(), limitKey); err != nil {
		writeError(w, http.StatusRequestTimeout, "unlock request timed out")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.database != nil {
		targetPath, targetID, err := s.unlockTargetPathLocked(request.DatabaseID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if runtime := s.workspaces[targetID]; runtime != nil {
			targetPath = runtime.path
		} else {
			s.activeDataPath = targetPath
			s.activeDatabase = targetID
			if err := s.openUnlockedLocked(request.Password); err != nil {
				s.authLimiter.recordFailure(limitKey)
				writeDatabaseUnlockError(w, err)
				return
			}
			s.authLimiter.recordSuccess(limitKey)
			if err := s.issueUISession(w); err != nil {
				writeInternalError(w)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status":      "unlocked",
				"state":       "unlocked",
				"unlocked_at": time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		if err := db.ValidateEncrypted(targetPath, request.Password); err != nil {
			s.authLimiter.recordFailure(limitKey)
			writeError(w, http.StatusUnauthorized, "invalid unlock password or database")
			return
		}
		if runtime := s.workspaces[targetID]; runtime != nil {
			s.applyRuntimeLocked(runtime)
		}
		s.authLimiter.recordSuccess(limitKey)
		if err := s.issueUISession(w); err != nil {
			writeInternalError(w)
			return
		}
		writeJSON(w, http.StatusOK, s.currentUnlockStatusLocked())
		return
	}
	targetPath, targetID, err := s.unlockTargetPathLocked(request.DatabaseID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.activeDataPath = targetPath
	s.activeDatabase = targetID

	if !db.Exists(targetPath) {
		writeError(w, http.StatusNotFound, "encrypted database is not initialized")
		return
	}
	if rejectPlaintextDatabase(w, targetPath) {
		return
	}
	if err := s.openUnlockedLocked(request.Password); err != nil {
		s.authLimiter.recordFailure(limitKey)
		writeDatabaseUnlockError(w, err)
		return
	}
	s.authLimiter.recordSuccess(limitKey)
	if err := s.issueUISession(w); err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "unlocked",
		"state":       "unlocked",
		"unlocked_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s unlockHandlers) lock(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Scope string `json:"scope"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
	}
	request.Scope = strings.TrimSpace(request.Scope)
	if request.Scope == "" {
		request.Scope = "current"
	}
	if request.Scope != "current" && request.Scope != "all" {
		writeError(w, http.StatusBadRequest, "scope must be current or all")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if request.Scope == "all" {
		s.closeAllUnlockedResources()
		s.clearUISessions(w)
	} else {
		s.closeActiveRuntimeLocked(true)
		if s.database == nil {
			s.clearUISessions(w)
		} else if err := s.issueUISession(w); err != nil {
			writeInternalError(w)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.currentUnlockStatusLocked())
}
