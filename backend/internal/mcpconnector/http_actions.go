package mcpconnector

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
)

const (
	maxActionBodyBytes       = 32 << 20
	maxReasonBytes           = 2 << 10
	actionRequestsPerMinute  = 60
	actionConcurrentRequests = 4
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

type ActionResourcePolicy struct {
	MaxInputBytes int
}

type ActionScope struct {
	Database       *sql.DB
	RuntimeID      string
	TokenID        int64
	Output         *OutputAuthorization
	ActionVisible  func(context.Context, string, string) (bool, error)
	ReplayExists   func(context.Context, string) (bool, error)
	ResourcePolicy func(context.Context, string, string) (ActionResourcePolicy, error)
	Call           func(context.Context, ActionCall) (ActionCallResult, error)
	Observe        func(context.Context, string, any)
	Redact         func(context.Context, string) string
	RunningHint    func(connectortargets.ActionRequest) string
}

type ActionScopeProvider func(http.ResponseWriter, *http.Request) (ActionScope, bool)

type ActionHTTPHandlers struct {
	scope     ActionScopeProvider
	admission *runtimecontrol.Admission
}

func NewActionHTTPHandlers(scope ActionScopeProvider) *ActionHTTPHandlers {
	return &ActionHTTPHandlers{
		scope:     scope,
		admission: runtimecontrol.NewAdmission(actionRequestsPerMinute, time.Minute, actionConcurrentRequests),
	}
}

func (h *ActionHTTPHandlers) Call(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r, true)
	if !ok {
		return
	}
	admissionKey := "runtime:" + scope.RuntimeID + ":token:" + strconv.FormatInt(scope.TokenID, 10)
	release, retryAfter, admitted := h.admission.Acquire(admissionKey)
	if !admitted {
		writeResourceLimit(w, retryAfter)
		return
	}
	defer release()
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
	replay, err := scope.ReplayExists(r.Context(), request.IdempotencyKey)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	policy := ActionResourcePolicy{MaxInputBytes: connectors.MaximumActionInputBytes}
	if !replay {
		visible, visibleErr := scope.ActionVisible(r.Context(), request.TargetRef, request.ActionName)
		if visibleErr != nil {
			httptransport.WriteInternalError(w)
			return
		}
		if !visible {
			httptransport.WriteError(w, http.StatusNotFound, "connector target not found")
			return
		}
		policy, err = scope.ResourcePolicy(r.Context(), request.TargetRef, request.ActionName)
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
	}
	inputJSON, err := json.Marshal(request.Input)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, "input must be valid JSON")
		return
	}
	if policy.MaxInputBytes < 1 || len(inputJSON) > policy.MaxInputBytes {
		writeCodedError(w, http.StatusRequestEntityTooLarge, "connector action input exceeds its size limit", "action_input_too_large")
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
	response := ResponseFromResult(scope.RunningHint, result.Request, result.Result)
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
	response := ResponseFromResult(scope.RunningHint, request, connectors.ActionResult{
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
	if scope.Database == nil || strings.TrimSpace(scope.RuntimeID) == "" || scope.TokenID < 1 || scope.Output == nil || !scope.Output.valid() || scope.Output.Database != scope.Database ||
		requireCall && (scope.ActionVisible == nil || scope.ReplayExists == nil || scope.ResourcePolicy == nil || scope.Call == nil || scope.Observe == nil || scope.Redact == nil) {
		httptransport.WriteInternalError(w)
		return ActionScope{}, false
	}
	return scope, true
}

func writeResourceLimit(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int64((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 60 {
		seconds = 60
	}
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	writeCodedError(w, http.StatusTooManyRequests, "connector action capacity is temporarily exhausted", "connector_action_backpressure")
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
	case errors.Is(err, connectortargets.ErrActionRequestCapacity):
		writeResourceLimit(w, time.Minute)
	case errors.Is(err, connectortargets.ErrInvalidTargetRef), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		writeTargetError(w, err)
	default:
		writeCodedError(w, http.StatusBadRequest, scope.Redact(r.Context(), err.Error()), connectors.ErrorCode(err))
	}
}

func writeCodedError(w http.ResponseWriter, status int, message, code string) {
	httptransport.WriteJSON(w, status, httptransport.ErrorResponse{Error: message, Code: code})
}

func ResponseFromResult(resolveRunningHint func(connectortargets.ActionRequest) string, request connectortargets.ActionRequest, result connectors.ActionResult) actions.Response {
	return actions.FromResult(request, result, responseRunningHint(resolveRunningHint, request))
}

func ResponseFromRequest(resolveRunningHint func(connectortargets.ActionRequest) string, request connectortargets.ActionRequest) actions.Response {
	return actions.FromRequest(request, responseRunningHint(resolveRunningHint, request))
}

func responseRunningHint(resolveRunningHint func(connectortargets.ActionRequest) string, request connectortargets.ActionRequest) string {
	if request.Status != connectors.ResultRunning {
		return ""
	}
	if resolveRunningHint != nil {
		if hint := strings.TrimSpace(resolveRunningHint(request)); hint != "" {
			return hint
		}
	}
	return "Wait 3 seconds, then call get_connector_action_request again until this request is completed, failed, canceled, stale, or error."
}
