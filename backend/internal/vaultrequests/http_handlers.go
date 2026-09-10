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
	Runtime    func(context.Context) (*Runtime, error)
}

type HTTPScopeProvider func(http.ResponseWriter) (HTTPScope, bool)

type HTTPHandlers struct {
	scope HTTPScopeProvider
}

type DecisionHTTPRequest struct {
	UserNote string `json:"user_note"`
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
	item, err := runtime.DeclinePending(r.Context(), id, request.UserNote)
	if writeDecisionHTTPError(w, err) {
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
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

func resolveHTTPRuntime(w http.ResponseWriter, r *http.Request, scope HTTPScope) (*Runtime, bool) {
	runtime, err := scope.Runtime(r.Context())
	if err != nil || runtime == nil || runtime.validate() != nil {
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
