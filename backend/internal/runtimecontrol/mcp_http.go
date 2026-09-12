package runtimecontrol

import (
	"context"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type MCPRuntimeState interface {
	MCPStarted() bool
	SetMCPStarted(bool)
}

type MCPRuntimeScope struct {
	State        MCPRuntimeState
	StartEnabled func(context.Context) (bool, error)
	AcquireStop  func(context.Context) (func(), error)
	StopEffects  func(context.Context) error
	Observe      func(context.Context, string, map[string]any)
	Now          func() time.Time
}

type MCPRuntimeScopeProvider func(http.ResponseWriter) (MCPRuntimeScope, bool)

type MCPHTTPHandlers struct{ scope MCPRuntimeScopeProvider }

type mcpHTTPRequirement uint8

const (
	requireMCPObserve mcpHTTPRequirement = 1 << iota
)

type MCPRuntimeResponse struct {
	Enabled      bool   `json:"enabled"`
	StartEnabled bool   `json:"start_enabled"`
	UpdatedAt    string `json:"updated_at"`
}

type UpdateMCPRuntimeRequest struct {
	Enabled bool `json:"enabled"`
}

func NewMCPHTTPHandlers(scope MCPRuntimeScopeProvider) *MCPHTTPHandlers {
	return &MCPHTTPHandlers{scope: scope}
}

func (h *MCPHTTPHandlers) Get(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, 0)
	if !ok {
		return
	}
	h.writeResponse(w, r, scope)
}

func (h *MCPHTTPHandlers) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireMCPObserve)
	if !ok {
		return
	}
	var request UpdateMCPRuntimeRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if !request.Enabled && (scope.AcquireStop == nil || scope.StopEffects == nil) {
		httptransport.WriteInternalError(w)
		return
	}
	var release func()
	if !request.Enabled {
		var err error
		release, err = scope.AcquireStop(r.Context())
		if err != nil {
			httptransport.WriteError(w, http.StatusRequestTimeout, "MCP stop was canceled")
			return
		}
		if release == nil {
			httptransport.WriteInternalError(w)
			return
		}
		defer release()
	}
	scope.State.SetMCPStarted(request.Enabled)
	if !request.Enabled {
		if err := scope.StopEffects(r.Context()); err != nil {
			httptransport.WriteInternalError(w)
			return
		}
	}
	action := "mcp.runtime.stopped"
	if request.Enabled {
		action = "mcp.runtime.started"
	}
	scope.Observe(r.Context(), action, map[string]any{"enabled": request.Enabled})
	h.writeResponse(w, r, scope)
}

func (h *MCPHTTPHandlers) resolve(w http.ResponseWriter, requirement mcpHTTPRequirement) (MCPRuntimeScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return MCPRuntimeScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return MCPRuntimeScope{}, false
	}
	if scope.State == nil || scope.StartEnabled == nil ||
		requirement&requireMCPObserve != 0 && scope.Observe == nil {
		httptransport.WriteInternalError(w)
		return MCPRuntimeScope{}, false
	}
	return scope, true
}

func (h *MCPHTTPHandlers) writeResponse(w http.ResponseWriter, r *http.Request, scope MCPRuntimeScope) {
	startEnabled, err := scope.StartEnabled(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, MCPRuntimeResponse{
		Enabled: scope.State.MCPStarted(), StartEnabled: startEnabled,
		UpdatedAt: scope.now().UTC().Format(time.RFC3339),
	})
}

func (scope MCPRuntimeScope) now() time.Time {
	if scope.Now != nil {
		return scope.Now()
	}
	return time.Now()
}
