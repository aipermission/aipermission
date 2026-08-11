package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type connectorActionHandlers struct{ *Server }

type localConnectorActionRequest struct {
	TargetRef      string         `json:"target_ref"`
	ActionName     string         `json:"action_name"`
	Input          map[string]any `json:"input,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

func (s connectorActionHandlers) runLocalConnectorAction(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request localConnectorActionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.TargetRef = strings.TrimSpace(request.TargetRef)
	request.ActionName = strings.TrimSpace(request.ActionName)
	request.Reason = strings.TrimSpace(request.Reason)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.TargetRef == "" {
		writeError(w, http.StatusBadRequest, "target_ref is required")
		return
	}
	if !connectors.ValidIdentifier(request.ActionName) {
		writeError(w, http.StatusBadRequest, "invalid action_name")
		return
	}
	if err := validateTextLimit("reason", request.Reason, maxReasonBytes); err != nil {
		writeError(w, http.StatusBadRequest, s.redactForPersistence(r.Context(), runtime, err.Error()))
		return
	}
	if len(request.IdempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "idempotency_key is too long")
		return
	}
	result, err := s.Server.runLocalConnectorAction(r.Context(), runtime, connectorActionCall{
		Source:         commandRequestSourceManual,
		TargetRef:      request.TargetRef,
		ActionName:     request.ActionName,
		Input:          request.Input,
		Reason:         request.Reason,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, connectortargets.ErrActionRequestIdempotency) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, connectortargets.ErrInvalidTargetRef) || errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
			handleConnectorTargetError(w, err)
			return
		}
		writeErrorWithCode(w, http.StatusBadRequest, s.redactForPersistence(r.Context(), runtime, err.Error()), connectors.ErrorCode(err))
		return
	}
	auditAction := "connector_action.manual." + string(result.Result.Status)
	if result.Replayed {
		auditAction = "connector_action.manual.replayed"
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, auditAction, map[string]any{
		"request_id":     result.Request.ID,
		"target_ref":     request.TargetRef,
		"connector_kind": result.Request.ConnectorKind,
		"action_name":    request.ActionName,
		"replayed":       result.Replayed,
	})
	response := connectorActionToMCPResponse(result.Request, result.Result)
	response.Replayed = result.Replayed
	writeJSON(w, http.StatusOK, response)
}
