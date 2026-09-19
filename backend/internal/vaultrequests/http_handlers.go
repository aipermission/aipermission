package vaultrequests

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type HTTPScope struct {
	MCPStarted func() bool
	Runtime    func(context.Context) (Application, error)
}

type Application interface {
	List(context.Context, string, int) ([]Request, error)
	Get(context.Context, int64) (Request, error)
	RunPending(context.Context, int64, string) (WorkflowResult, error)
	DeclinePending(context.Context, int64, string) (Request, error)
	Call(context.Context, CallInput) (RequestView, error)
	GetOwned(context.Context, int64, int64) (RequestView, error)
	CancelOwned(context.Context, int64, int64) (Request, error)
	StalePendingForContext(context.Context, int64, int64, string) error
	StalePendingForProject(context.Context, int64, string) error
	StalePendingForRuntimes(context.Context, []int64, string) error
	StalePendingForAction(context.Context, string, string) error
	FailRunning(context.Context, string) error
	Validate() error
}

type HTTPScopeProvider func(http.ResponseWriter) (HTTPScope, bool)

type HTTPHandlers struct {
	scope HTTPScopeProvider
}

type DecisionHTTPRequest struct {
	UserNote            string `json:"user_note"`
	ApprovalContextHash string `json:"approval_context_hash"`
}

func NewHTTPHandlers(scope HTTPScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope}
}

func (h *HTTPHandlers) List(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	runtime, ok := resolveHTTPRuntime(w, r, scope)
	if !ok {
		return
	}
	items, err := runtime.List(r.Context(), strings.TrimSpace(r.URL.Query().Get("status")), 100)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, items)
}

func (h *HTTPHandlers) Run(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	if !scope.MCPStarted() {
		httptransport.WriteError(w, http.StatusConflict, ErrMCPExecutionStopped.Error())
		return
	}
	request, ok := decodeDecisionHTTPRequest(w, r)
	if !ok {
		return
	}
	runtime, ok := resolveHTTPRuntime(w, r, scope)
	if !ok {
		return
	}
	if !validateVaultDecisionContext(w, r, runtime, id, request.ApprovalContextHash) {
		return
	}
	result, err := runtime.RunPending(r.Context(), id, request.UserNote)
	if writeDecisionHTTPError(w, err) {
		return
	}
	if result.ExecutionError != nil {
		httptransport.WriteError(w, http.StatusConflict, result.Request.Error)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, result.Request)
}

func (h *HTTPHandlers) Decline(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	request, ok := decodeDecisionHTTPRequest(w, r)
	if !ok {
		return
	}
	runtime, ok := resolveHTTPRuntime(w, r, scope)
	if !ok {
		return
	}
	if !validateVaultDecisionContext(w, r, runtime, id, request.ApprovalContextHash) {
		return
	}
	item, err := runtime.DeclinePending(r.Context(), id, request.UserNote)
	if writeDecisionHTTPError(w, err) {
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func validateVaultDecisionContext(w http.ResponseWriter, r *http.Request, runtime Application, id int64, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "approval_context_hash is required")
		return false
	}
	item, err := runtime.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httptransport.WriteError(w, http.StatusNotFound, ErrNotFound.Error())
		return false
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return false
	}
	if item.ApprovalContextHash == "" || item.ApprovalContextHash != expected {
		httptransport.WriteError(w, http.StatusConflict, "Vault approval context changed; refresh and review the request again")
		return false
	}
	return true
}

func (h *HTTPHandlers) resolve(w http.ResponseWriter) (HTTPScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return HTTPScope{}, false
	}
	if scope.MCPStarted == nil || scope.Runtime == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	return scope, true
}

func resolveHTTPRuntime(w http.ResponseWriter, r *http.Request, scope HTTPScope) (Application, bool) {
	runtime, err := scope.Runtime(r.Context())
	if err != nil || runtime == nil || runtime.Validate() != nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return runtime, true
}

func decodeDecisionHTTPRequest(w http.ResponseWriter, r *http.Request) (DecisionHTTPRequest, bool) {
	request := DecisionHTTPRequest{}
	if r.ContentLength != 0 && !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return DecisionHTTPRequest{}, false
	}
	userNote, err := normalizeUserNote(request.UserNote)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return DecisionHTTPRequest{}, false
	}
	request.UserNote = userNote
	return request, true
}

func writeDecisionHTTPError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var validation ValidationError
	switch {
	case errors.Is(err, ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "Vault action request not found")
	case errors.Is(err, ErrNotPending):
		httptransport.WriteError(w, http.StatusConflict, "Vault action request is no longer pending")
	case errors.Is(err, ErrMCPExecutionStopped):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		httptransport.WriteInternalError(w)
	}
	return true
}
