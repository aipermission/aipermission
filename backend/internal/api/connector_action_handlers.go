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
	TargetRef  string         `json:"target_ref"`
	ActionName string         `json:"action_name"`
	Input      map[string]any `json:"input,omitempty"`
	Reason     string         `json:"reason,omitempty"`
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
	result, err := s.Server.runLocalConnectorAction(r.Context(), runtime, connectorActionCall{
		Source:     commandRequestSourceManual,
		TargetRef:  request.TargetRef,
		ActionName: request.ActionName,
		Input:      request.Input,
		Reason:     request.Reason,
	})
	if err != nil {
		if errors.Is(err, connectortargets.ErrInvalidTargetRef) || errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
			handleConnectorTargetError(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, s.redactForPersistence(r.Context(), runtime, err.Error()))
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector_action.manual."+string(result.Result.Status), map[string]any{
		"request_id":     result.Request.ID,
		"target_ref":     request.TargetRef,
		"connector_kind": result.Request.ConnectorKind,
		"action_name":    request.ActionName,
	})
	writeJSON(w, http.StatusOK, connectorActionToMCPResponse(result.Request, result.Result))
}
