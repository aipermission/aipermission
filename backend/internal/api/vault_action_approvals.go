package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type vaultActionDecisionRequest struct {
	UserNote string `json:"user_note"`
}

func (s vaultActionApprovalHandlers) list(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	items, err := vaultrequests.NewStore(runtime.database).List(r.Context(), strings.TrimSpace(r.URL.Query().Get("status")), 100)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s vaultActionApprovalHandlers) run(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	if !runtime.isMCPStarted() {
		writeError(w, http.StatusConflict, "MCP execution is stopped; start MCP before running Vault approvals")
		return
	}
	request, ok := decodeVaultDecision(w, r)
	if !ok {
		return
	}
	executionContext, cancelExecution := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancelExecution()
	result, err := s.runVaultActionRequest(
		executionContext,
		runtime,
		id,
		"user",
		request.UserNote,
		"vault.action.run_requested",
		"vault.action",
	)
	if errors.Is(err, vaultrequests.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Vault action request not found")
		return
	}
	if errors.Is(err, vaultrequests.ErrNotPending) {
		writeError(w, http.StatusConflict, "Vault action request is no longer pending")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if result.ExecutionError != nil {
		writeError(w, http.StatusConflict, result.Request.Error)
		return
	}
	writeJSON(w, http.StatusOK, result.Request)
}

func (s vaultActionApprovalHandlers) decline(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	request, ok := decodeVaultDecision(w, r)
	if !ok {
		return
	}
	item, err := vaultrequests.NewStore(runtime.database).Decline(r.Context(), id, request.UserNote)
	if errors.Is(err, vaultrequests.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Vault action request not found")
		return
	}
	if errors.Is(err, vaultrequests.ErrNotPending) {
		writeError(w, http.StatusConflict, "Vault action request is no longer pending")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", int64Ptr(item.TokenID), valueOrZero(item.RuntimeID), "vault.action.declined", vaultActionAuditPayload(item, request.UserNote))
	writeJSON(w, http.StatusOK, item)
}

func decodeVaultDecision(w http.ResponseWriter, r *http.Request) (vaultActionDecisionRequest, bool) {
	var request vaultActionDecisionRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return vaultActionDecisionRequest{}, false
		}
	}
	request.UserNote = strings.TrimSpace(request.UserNote)
	if err := validateTextLimit("user_note", request.UserNote, maxMessageBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return vaultActionDecisionRequest{}, false
	}
	return request, true
}

func vaultActionAuditPayload(item vaultrequests.Request, userNote string) map[string]any {
	return map[string]any{
		"request_id": item.ID, "project_id": item.ProjectID, "action_name": item.ActionName,
		"approval_context_hash": item.ApprovalContextHash, "note": strings.TrimSpace(userNote) != "",
	}
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
