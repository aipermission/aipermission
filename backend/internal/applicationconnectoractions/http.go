package applicationconnectoractions

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type LocalHTTPDependencies struct {
	ActiveRuntime     func(http.ResponseWriter) (workspaceruntime.Port, bool)
	DecodeJSON        func(http.ResponseWriter, *http.Request, any) bool
	WriteError        func(http.ResponseWriter, int, string)
	WriteErrorCode    func(http.ResponseWriter, int, string, string)
	WriteJSON         func(http.ResponseWriter, int, any)
	HandleTargetError func(http.ResponseWriter, error)
	Response          func(connectortargets.ActionRequest, connectors.ActionResult, bool) any
}

type LocalHTTPHandlers struct {
	component    *Component
	dependencies LocalHTTPDependencies
}

func (component *Component) LocalHTTP(dependencies LocalHTTPDependencies) LocalHTTPHandlers {
	return LocalHTTPHandlers{component: component, dependencies: dependencies}
}

type LocalRequest struct {
	TargetRef      string         `json:"target_ref"`
	ActionName     string         `json:"action_name"`
	Input          map[string]any `json:"input,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

func (handlers LocalHTTPHandlers) Run(w http.ResponseWriter, r *http.Request) {
	runtime, ok := handlers.dependencies.ActiveRuntime(w)
	if !ok {
		return
	}
	var request LocalRequest
	if !handlers.dependencies.DecodeJSON(w, r, &request) {
		return
	}
	request.TargetRef, request.ActionName = strings.TrimSpace(request.TargetRef), strings.TrimSpace(request.ActionName)
	request.Reason, request.IdempotencyKey = strings.TrimSpace(request.Reason), strings.TrimSpace(request.IdempotencyKey)
	if request.TargetRef == "" {
		handlers.dependencies.WriteError(w, http.StatusBadRequest, "target_ref is required")
		return
	}
	if !connectors.ValidIdentifier(request.ActionName) {
		handlers.dependencies.WriteError(w, http.StatusBadRequest, "invalid action_name")
		return
	}
	if len([]byte(request.Reason)) > 2<<10 {
		handlers.dependencies.WriteError(w, http.StatusBadRequest, handlers.component.dependencies.RedactBasic(r.Context(), runtime, "reason must be 2048 bytes or less"))
		return
	}
	if len(request.IdempotencyKey) > connectortargets.MaxIdempotencyKeyBytes {
		handlers.dependencies.WriteError(w, http.StatusBadRequest, "idempotency_key is too long")
		return
	}
	workflow, err := handlers.component.Workflow(runtime)
	if err == nil {
		var result actions.CallResult
		result, err = workflow.RunLocal(r.Context(), actions.Call{
			Source: actions.SourceManual, TargetRef: request.TargetRef, ActionName: request.ActionName,
			Input: request.Input, Reason: request.Reason, IdempotencyKey: request.IdempotencyKey,
		})
		if err == nil {
			auditAction := "connector_action.manual." + string(result.Result.Status)
			if result.Replayed {
				auditAction = "connector_action.manual.replayed"
			}
			handlers.component.dependencies.Observe(r.Context(), runtime, "user", nil, 0, auditAction, map[string]any{
				"request_id": result.Request.ID, "target_ref": request.TargetRef, "connector_kind": result.Request.ConnectorKind,
				"action_name": request.ActionName, "replayed": result.Replayed,
			})
			handlers.dependencies.WriteJSON(w, http.StatusOK, handlers.dependencies.Response(result.Request, result.Result, result.Replayed))
			return
		}
	}
	var persistence *actions.TerminalPersistenceError
	if errors.As(err, &persistence) {
		handlers.dependencies.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": connectors.ResultOutcomeUnknown, "code": "connector_action_persistence_unknown", "request_id": persistence.RequestID,
			"error":          actions.TerminalPersistenceErrorText,
			"assistant_hint": "Do not retry automatically. Inspect the recorded request and external target state first.",
		})
		return
	}
	if errors.Is(err, connectortargets.ErrActionRequestIdempotency) {
		handlers.dependencies.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, connectortargets.ErrInvalidTargetRef) || errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
		handlers.dependencies.HandleTargetError(w, err)
		return
	}
	handlers.dependencies.WriteErrorCode(w, http.StatusBadRequest, handlers.component.dependencies.RedactBasic(r.Context(), runtime, err.Error()), connectors.ErrorCode(err))
}
