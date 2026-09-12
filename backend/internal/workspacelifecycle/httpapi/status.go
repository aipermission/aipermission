package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

func (h *Handlers) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.dependencies.Lifecycle.Status()
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	response := publicStatus(status)
	if response.State == "unlocked" && (h.dependencies.HasSession == nil || !h.dependencies.HasSession(r)) {
		response.State = "session_required"
	}
	httptransport.WriteJSON(w, http.StatusOK, response)
}

func (h *Handlers) Setup(w http.ResponseWriter, r *http.Request) {
	var request SetupRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.Password, &request.ConfirmPassword)
	if err := workspacelifecycle.ValidatePassword(request.Password, request.ConfirmPassword); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	transition, err := h.dependencies.Lifecycle.Setup(request.DatabaseID, request.DatabaseName, request.Password)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if transition.Status == "current" {
		status, statusErr := h.dependencies.Lifecycle.Status()
		if statusErr != nil {
			httptransport.WriteInternalError(w)
			return
		}
		httptransport.WriteJSON(w, http.StatusOK, publicStatus(status))
		return
	}
	if !h.issueSession(w) {
		return
	}
	h.writeTransition(w, "unlocked", transition.Identity.ID, "unlocked_at")
}

func (h *Handlers) Unlock(w http.ResponseWriter, r *http.Request) {
	var request UnlockRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.Password)
	if request.Password == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "password is required")
		return
	}
	attempt, ok := h.beginAttempt(w, r)
	if !ok {
		return
	}
	transition, err := h.dependencies.Lifecycle.Unlock(request.DatabaseID, request.Password)
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrCredential) || errors.Is(err, workspacelifecycle.ErrAuthentication) {
			attempt.Failure()
		} else {
			attempt.Success()
		}
		writeUnlockError(w, err)
		return
	}
	attempt.Success()
	if !h.issueSession(w) {
		return
	}
	if transition.Opened {
		h.writeTransition(w, "unlocked", transition.Identity.ID, "unlocked_at")
		return
	}
	status, err := h.dependencies.Lifecycle.Status()
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, publicStatus(status))
}

func (h *Handlers) Lock(w http.ResponseWriter, r *http.Request) {
	request := struct {
		Scope string `json:"scope"`
	}{}
	if r.Body != nil && r.ContentLength != 0 && !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	request.Scope = strings.TrimSpace(request.Scope)
	if request.Scope == "" {
		request.Scope = "current"
	}
	if request.Scope != "current" && request.Scope != "all" {
		httptransport.WriteError(w, http.StatusBadRequest, workspacelifecycle.ErrInvalidScope.Error())
		return
	}
	if h.dependencies.Lifecycle.WillLockAll(request.Scope) && h.dependencies.CloseMaintenance != nil {
		h.dependencies.CloseMaintenance("database_lock_" + request.Scope)
	}
	status, err := h.dependencies.Lifecycle.Lock(request.Scope)
	if status.State != "unlocked" || request.Scope == "all" {
		if h.dependencies.ClearSessions != nil {
			h.dependencies.ClearSessions(w)
		}
	}
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrInvalidScope) {
			httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		} else {
			httptransport.WriteInternalError(w)
		}
		return
	}
	if status.State == "unlocked" && request.Scope != "all" {
		if !h.issueSession(w) {
			return
		}
	}
	httptransport.WriteJSON(w, http.StatusOK, publicStatus(status))
}

func publicStatus(status workspacelifecycle.Status) StatusResponse {
	databases := append([]databasecatalog.DatabaseInfo(nil), status.Databases...)
	for index := range databases {
		databases[index].Path = ""
	}
	return StatusResponse{
		State: status.State, DatabaseID: status.Identity.ID, DatabaseName: status.DatabaseName,
		DatabaseSizeBytes: status.DatabaseSizeBytes, UISessionAuthenticated: status.State == "unlocked",
		Databases: databases,
	}
}

func (h *Handlers) issueSession(w http.ResponseWriter) bool {
	if h.dependencies.IssueSession == nil || h.dependencies.IssueSession(w) != nil {
		httptransport.WriteInternalError(w)
		return false
	}
	return true
}

func (h *Handlers) writeTransition(w http.ResponseWriter, status, databaseID, timestampKey string) {
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status": status, "state": "unlocked", "database_id": databaseID,
		timestampKey: h.now().UTC().Format(time.RFC3339),
	})
}
