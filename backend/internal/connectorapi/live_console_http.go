// Package connectorapi owns generic connector-facing HTTP contracts, including
// interactive session transport. The console package remains the lower-level
// PTY/session engine; composition roots provide workspace-scoped dependencies.
package connectorapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/gorilla/websocket"
)

type LiveConsoleSessions interface {
	List(context.Context, int64) ([]console.Record, error)
	Create(context.Context, console.CreateRequest) (console.Record, error)
	Get(context.Context, int64) (console.Record, error)
	Input(context.Context, executionprincipal.Principal, int64, string) error
	Close(context.Context, executionprincipal.Principal, int64) error
	RuntimeID(context.Context, int64) (int64, error)
	Attach(http.ResponseWriter, *http.Request, executionprincipal.Principal, int64, func(http.ResponseWriter, *http.Request) (*websocket.Conn, error)) error
}

type LiveConsoleEnvironmentPlan struct {
	ItemIDs     []int64
	ContentHash string
	Prepare     console.EnvironmentPreparer
}

type LiveConsoleRestartResult struct {
	ClosedSessionIDs        []int64
	CanceledRunningRequests int64
}

type LiveConsoleHTTPRuntime struct {
	Sessions         LiveConsoleSessions
	Principal        func() (executionprincipal.Principal, error)
	PlanEnvironment  func(context.Context, int64, []projectvault.SessionSelection) (LiveConsoleEnvironmentPlan, error)
	ErrorAdapter     func(context.Context, int64) ErrorPresenter
	CancelForSession func(context.Context, int64, string) error
	RestartRuntime   func(context.Context, int64, string) (LiveConsoleRestartResult, error)
	Observe          func(context.Context, int64, string, map[string]any)
	UpgradeWebSocket func(http.ResponseWriter, *http.Request) (*websocket.Conn, error)
}

type LiveConsoleHTTPScopeProvider func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool)

type LiveConsoleHTTPHandlers struct {
	scope LiveConsoleHTTPScopeProvider
}

type LiveConsoleCreateHTTPRequest struct {
	RuntimeID     int64                           `json:"runtime_id"`
	Name          string                          `json:"name"`
	CloseExisting bool                            `json:"close_existing"`
	Cols          int                             `json:"cols"`
	Rows          int                             `json:"rows"`
	Params        map[string]any                  `json:"params,omitempty"`
	VaultItems    []projectvault.SessionSelection `json:"vault_items,omitempty"`
}

func NewLiveConsoleHTTPHandlers(scope LiveConsoleHTTPScopeProvider) *LiveConsoleHTTPHandlers {
	return &LiveConsoleHTTPHandlers{scope: scope}
}

func (h *LiveConsoleHTTPHandlers) List(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	runtimeID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("runtime_id")); raw != "" {
		var parsed bool
		runtimeID, parsed = httptransport.ParseQueryInt64(w, raw, "runtime_id")
		if !parsed {
			return
		}
	}
	items, err := runtime.Sessions.List(r.Context(), runtimeID)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, items)
}

func (h *LiveConsoleHTTPHandlers) Create(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	var input LiveConsoleCreateHTTPRequest
	if !httptransport.DecodeJSON(w, r, &input, httptransport.DefaultJSONBodyBytes) {
		return
	}
	request := console.CreateRequest{
		RuntimeID: input.RuntimeID, Name: input.Name, CloseExisting: input.CloseExisting,
		Cols: input.Cols, Rows: input.Rows, Params: input.Params,
		WaitForStart: true,
	}
	principal, err := runtime.principal()
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	request.Principal = principal
	var environment LiveConsoleEnvironmentPlan
	if len(input.VaultItems) > 0 {
		if runtime.PlanEnvironment == nil {
			httptransport.WriteInternalError(w)
			return
		}
		var err error
		environment, err = runtime.PlanEnvironment(r.Context(), input.RuntimeID, input.VaultItems)
		if err != nil {
			writeEnvironmentError(w, err)
			return
		}
		request.PrepareEnvironment = environment.Prepare
		request.EnvironmentContentHash = environment.ContentHash
	}
	item, err := runtime.Sessions.Create(r.Context(), request)
	if errors.Is(err, console.ErrSessionLimit) {
		httptransport.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		var adapter ErrorPresenter
		if runtime.ErrorAdapter != nil {
			adapter = runtime.ErrorAdapter(r.Context(), request.RuntimeID)
		}
		if WritePresentedError(w, adapter, err) {
			return
		}
		httptransport.WriteError(w, http.StatusBadRequest, PresentedErrorMessage(adapter, "console session failed", err))
		return
	}
	runtime.observe(r.Context(), item.RuntimeID, "console.session.created_observed", map[string]any{
		"session_id":               item.ID,
		"name":                     item.Name,
		"close_existing":           request.CloseExisting,
		"vault_item_ids":           environment.ItemIDs,
		"environment_content_hash": environment.ContentHash,
	})
	httptransport.WriteJSON(w, http.StatusCreated, item)
}

func (h *LiveConsoleHTTPHandlers) Get(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	item, err := runtime.Sessions.Get(r.Context(), id)
	if err != nil {
		httptransport.WriteError(w, http.StatusNotFound, "console session not found")
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *LiveConsoleHTTPHandlers) Input(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	var request console.InputRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	principal, err := runtime.principal()
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if err := runtime.Sessions.Input(r.Context(), principal, id, request.Data); errors.Is(err, console.ErrInputTooLarge) {
		httptransport.WriteError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	} else if err != nil {
		httptransport.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if runtimeID, err := runtime.Sessions.RuntimeID(r.Context(), id); err == nil {
		runtime.observe(r.Context(), runtimeID, "console.session.input", map[string]any{
			"session_id": id,
			"bytes":      len(request.Data),
		})
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"status": "sent"})
}

func (h *LiveConsoleHTTPHandlers) Close(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	principal, err := runtime.principal()
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if err := runtime.Sessions.Close(r.Context(), principal, id); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if runtime.CancelForSession == nil || runtime.CancelForSession(r.Context(), id, "console session closed before command completed") != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if runtimeID, err := runtime.Sessions.RuntimeID(r.Context(), id); err == nil {
		runtime.observe(r.Context(), runtimeID, "console.session.closed_observed", map[string]any{"session_id": id})
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"status": "closed"})
}

func (h *LiveConsoleHTTPHandlers) Restart(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	runtimeID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if runtime.RestartRuntime == nil {
		httptransport.WriteInternalError(w)
		return
	}
	result, err := runtime.RestartRuntime(r.Context(), runtimeID, "console session restarted by local user before command completed")
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	runtime.observe(r.Context(), runtimeID, "console.session.restarted", map[string]any{
		"closed_session_ids":        result.ClosedSessionIDs,
		"canceled_running_requests": result.CanceledRunningRequests,
	})
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status":                    "restarted",
		"runtime_id":                runtimeID,
		"target_id":                 runtimeID,
		"closed_session_ids":        result.ClosedSessionIDs,
		"canceled_running_requests": result.CanceledRunningRequests,
	})
}

func (h *LiveConsoleHTTPHandlers) Attach(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if runtime.UpgradeWebSocket == nil {
		httptransport.WriteInternalError(w)
		return
	}
	principal, err := runtime.principal()
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	err = runtime.Sessions.Attach(w, r, principal, id, runtime.UpgradeWebSocket)
	switch {
	case errors.Is(err, console.ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "console session not found")
	case errors.Is(err, console.ErrClientLimit):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case err != nil:
		var inactive console.InactiveError
		if errors.As(err, &inactive) {
			httptransport.WriteError(w, http.StatusConflict, inactive.Error())
			return
		}
		httptransport.WriteInternalError(w)
	}
}

func (h *LiveConsoleHTTPHandlers) resolve(w http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	runtime, ok := h.scope(w)
	if !ok {
		return nil, false
	}
	if runtime == nil || runtime.Sessions == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return runtime, true
}

func (runtime *LiveConsoleHTTPRuntime) principal() (executionprincipal.Principal, error) {
	if runtime == nil || runtime.Principal == nil {
		return executionprincipal.Principal{}, executionprincipal.ErrInvalid
	}
	principal, err := runtime.Principal()
	if err != nil {
		return executionprincipal.Principal{}, err
	}
	if err := principal.Validate(); err != nil {
		return executionprincipal.Principal{}, err
	}
	return principal, nil
}

func (runtime *LiveConsoleHTTPRuntime) observe(ctx context.Context, runtimeID int64, action string, payload map[string]any) {
	if runtime != nil && runtime.Observe != nil {
		runtime.Observe(ctx, runtimeID, action, payload)
	}
}

func writeEnvironmentError(w http.ResponseWriter, err error) {
	var validation projectvault.ValidationError
	switch {
	case errors.Is(err, connectors.ErrSessionEnvironmentUnsupported):
		httptransport.WriteError(w, http.StatusConflict, "this connector runtime does not support Vault session environments")
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, projectvault.ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "vault item not found")
	case errors.Is(err, projectvault.ErrStale):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}
