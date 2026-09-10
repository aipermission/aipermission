package api

import (
	"errors"
	"net/http"
	"strings"

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
	owner, err := s.vaultRequestRuntime(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	items, err := owner.List(r.Context(), strings.TrimSpace(r.URL.Query().Get("status")), 100)
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
		writeError(w, http.StatusConflict, vaultrequests.ErrMCPExecutionStopped.Error())
		return
	}
	request, ok := decodeVaultDecision(w, r)
	if !ok {
		return
	}
	owner, err := s.vaultRequestRuntime(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	result, err := owner.RunPending(r.Context(), id, request.UserNote)
	if errors.Is(err, vaultrequests.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Vault action request not found")
		return
	}
	if errors.Is(err, vaultrequests.ErrNotPending) {
		writeError(w, http.StatusConflict, "Vault action request is no longer pending")
		return
	}
	if errors.Is(err, vaultrequests.ErrMCPExecutionStopped) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	var validation vaultrequests.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, err.Error())
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
	owner, err := s.vaultRequestRuntime(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := owner.DeclinePending(r.Context(), id, request.UserNote)
	if errors.Is(err, vaultrequests.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Vault action request not found")
		return
	}
	if errors.Is(err, vaultrequests.ErrNotPending) {
		writeError(w, http.StatusConflict, "Vault action request is no longer pending")
		return
	}
	var validation vaultrequests.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func decodeVaultDecision(w http.ResponseWriter, r *http.Request) (vaultActionDecisionRequest, bool) {
	var request vaultActionDecisionRequest
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &request) {
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

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
