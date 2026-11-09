package gatewayconnectorapi

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type LiveConsoleIdleCleanup interface {
	CleanupPolicy(context.Context) (capacity.CleanupPolicy, error)
	UpdateCleanupPolicy(context.Context, capacity.CleanupPolicy, capacity.MutationRunner) error
}

func (h *LiveConsoleHTTPHandlers) CleanupSettings(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	sessions, ok := runtime.Sessions.(LiveConsoleIdleCleanup)
	if !ok {
		httptransport.WriteInternalError(w)
		return
	}
	policy, err := sessions.CleanupPolicy(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, policy)
}

func (h *LiveConsoleHTTPHandlers) UpdateCleanupSettings(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	principal, err := runtime.principal()
	if err != nil || principal.Kind != PrincipalLocalOperator {
		httptransport.WriteError(w, http.StatusForbidden, "local operator required")
		return
	}
	sessions, ok := runtime.Sessions.(LiveConsoleIdleCleanup)
	if !ok || runtime.Mutate == nil {
		httptransport.WriteInternalError(w)
		return
	}
	var input struct {
		Enabled     *bool `json:"enabled"`
		IdleMinutes *int  `json:"idle_minutes"`
	}
	if !httptransport.DecodeJSON(w, r, &input, 1024) {
		return
	}
	if input.Enabled == nil || input.IdleMinutes == nil {
		httptransport.WriteError(w, http.StatusBadRequest, "enabled and idle_minutes are required")
		return
	}
	policy := capacity.CleanupPolicy{Enabled: *input.Enabled, IdleMinutes: *input.IdleMinutes}
	if !policy.Valid() {
		httptransport.WriteError(w, http.StatusBadRequest, "invalid idle threshold")
		return
	}
	if err := sessions.UpdateCleanupPolicy(r.Context(), policy, runtime.Mutate); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, policy)
}
