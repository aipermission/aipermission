package mcpconnector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const (
	maxActionBodyBytes = 32 << 20
	maxReasonBytes     = 2 << 10
)

type ActionCallRequest struct {
	TargetRef      string         `json:"target_ref"`
	ActionName     string         `json:"action_name"`
	Input          map[string]any `json:"input,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

type ActionCall struct {
	Source         string
	TokenID        int64
	TargetRef      string
	ActionName     string
	Input          map[string]any
	Reason         string
	IdempotencyKey string
}

type ActionCallResult struct {
	Request  connectortargets.ActionRequest
	Result   connectors.ActionResult
	Replayed bool
}

type ActionScope struct {
	Database        *sql.DB
	AdapterRegistry *connectorapi.Registry
	TokenID         int64
	Output          *OutputAuthorization
	Call            func(context.Context, ActionCall) (ActionCallResult, error)
	Observe         func(context.Context, string, any)
	Redact          func(context.Context, string) string
}

type ActionScopeProvider func(http.ResponseWriter, *http.Request) (ActionScope, bool)

type ActionHTTPHandlers struct{ scope ActionScopeProvider }

func NewActionHTTPHandlers(scope ActionScopeProvider) *ActionHTTPHandlers {
	return &ActionHTTPHandlers{scope: scope}
}

func (h *ActionHTTPHandlers) Call(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r, true)
	if !ok {
		return
	}
	var request ActionCallRequest
	if !httptransport.DecodeJSON(w, r, &request, maxActionBodyBytes) {
		return
	}
	request.TargetRef = strings.TrimSpace(request.TargetRef)
	request.ActionName = strings.TrimSpace(request.ActionName)
	request.Reason = strings.TrimSpace(request.Reason)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.TargetRef == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "target_ref is required")
		return
	}
	if !connectors.ValidIdentifier(request.ActionName) {
		httptransport.WriteError(w, http.StatusBadRequest, "invalid action_name")
		return
	}
	if len([]byte(request.Reason)) > maxReasonBytes {
		writeCodedError(w, http.StatusBadRequest, scope.Redact(r.Context(), fmt.Sprintf("reason must be %d bytes or less", maxReasonBytes)), "")
		return
	}
	if len(request.IdempotencyKey) > connectortargets.MaxIdempotencyKeyBytes {
		httptransport.WriteError(w, http.StatusBadRequest, "idempotency_key is too long")
		return
	}
	result, err := scope.Call(r.Context(), ActionCall{
		Source: "mcp", TokenID: scope.TokenID, TargetRef: request.TargetRef,
		ActionName: request.ActionName, Input: request.Input, Reason: request.Reason,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		writeActionError(w, r, scope, err)
		return
	}
	auditAction := "mcp.connector_action." + string(result.Result.Status)
	if result.Replayed {
		auditAction = "mcp.connector_action.replayed"
	}
	scope.Observe(r.Context(), auditAction, map[string]any{
		"request_id": result.Request.ID, "target_ref": request.TargetRef,
		"connector_kind": result.Request.ConnectorKind, "action_name": request.ActionName,
		"replayed": result.Replayed,
	})
	response := ResponseFromResult(scope.AdapterRegistry, result.Request, result.Result)
	response.Replayed = result.Replayed
	scope.Output.Deliver(w, r, scope.TokenID, result.Request, response)
}

func (h *ActionHTTPHandlers) GetRequest(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r, false)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	request, err := connectortargets.NewStore(scope.Database).GetActionRequest(r.Context(), id)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		httptransport.WriteError(w, http.StatusNotFound, "connector action request not found")
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if request.TokenID == nil || *request.TokenID != scope.TokenID {
		httptransport.WriteError(w, http.StatusNotFound, "connector action request not found")
		return
	}
	response := ResponseFromResult(scope.AdapterRegistry, request, connectors.ActionResult{
		Status: request.Status, Output: request.Output, DisplayText: request.DisplayText, Error: request.Error,
	})
	scope.Output.Deliver(w, r, scope.TokenID, request, response)
}

func (h *ActionHTTPHandlers) resolve(w http.ResponseWriter, r *http.Request, requireCall bool) (ActionScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return ActionScope{}, false
	}
	scope, ok := h.scope(w, r)
	if !ok {
		return ActionScope{}, false
	}
	if scope.Database == nil || scope.TokenID < 1 || scope.Output == nil || !scope.Output.valid() || scope.Output.Database != scope.Database ||
		requireCall && (scope.Call == nil || scope.Observe == nil || scope.Redact == nil) {
		httptransport.WriteInternalError(w)
		return ActionScope{}, false
	}
	return scope, true
}

func writeActionError(w http.ResponseWriter, r *http.Request, scope ActionScope, err error) {
	var persistenceErr *actions.TerminalPersistenceError
	switch {
	case errors.As(err, &persistenceErr):
		httptransport.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": connectors.ResultOutcomeUnknown, "code": "connector_action_persistence_unknown",
			"request_id": persistenceErr.RequestID, "error": actions.TerminalPersistenceErrorText,
			"assistant_hint": "Do not retry automatically. Inspect the recorded request and external target state first.",
		})
	case errors.Is(err, actions.ErrMCPExecutionStopped):
		httptransport.WriteJSON(w, http.StatusOK, map[string]any{
			"status": "stopped", "error": "MCP execution is stopped in the local gateway. Start MCP from the web UI before running commands.",
		})
	case errors.Is(err, connectortargets.ErrActionRequestIdempotency):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, connectortargets.ErrInvalidTargetRef), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		writeTargetError(w, err)
	default:
		writeCodedError(w, http.StatusBadRequest, scope.Redact(r.Context(), err.Error()), connectors.ErrorCode(err))
	}
}

func writeCodedError(w http.ResponseWriter, status int, message, code string) {
	httptransport.WriteJSON(w, status, httptransport.ErrorResponse{Error: message, Code: code})
}

func ResponseFromResult(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest, result connectors.ActionResult) actions.Response {
	return actions.FromResult(request, result, responseRunningHint(adapterRegistry, request))
}

func ResponseFromRequest(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest) actions.Response {
	return actions.FromRequest(request, responseRunningHint(adapterRegistry, request))
}

func responseRunningHint(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest) string {
	if request.Status != connectors.ResultRunning {
		return ""
	}
	adapter, _ := adapterRegistry.For(request.ConnectorKind).(connectorapi.RuntimeAdapter)
	if adapter != nil {
		if hint := strings.TrimSpace(adapter.RunningHint(request)); hint != "" {
			return hint
		}
	}
	return "Wait 3 seconds, then call get_connector_action_request again until this request is completed, failed, canceled, stale, or error."
}
