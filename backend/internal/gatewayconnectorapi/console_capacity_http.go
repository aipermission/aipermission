package gatewayconnectorapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

// Optional capability keeps capacity management separate from transport ports.
type LiveConsoleCapacity interface {
	Capacity(context.Context) (capacity.Report, error)
	WaitCapacity(context.Context, uint64) (uint64, error)
	CloseCapacitySession(context.Context, Principal, capacity.Selection, int) error
}

func (h *LiveConsoleHTTPHandlers) Capacity(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	sessions, ok := runtime.Sessions.(LiveConsoleCapacity)
	if !ok {
		httptransport.WriteInternalError(w)
		return
	}
	report, err := sessions.Capacity(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, report)
}

func (h *LiveConsoleHTTPHandlers) CloseCapacity(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	sessions, ok := runtime.Sessions.(LiveConsoleCapacity)
	if !ok {
		httptransport.WriteInternalError(w)
		return
	}
	principal, err := runtime.principal()
	if err != nil || principal.Kind != PrincipalLocalOperator {
		httptransport.WriteError(w, http.StatusForbidden, "local operator required")
		return
	}
	var input capacity.CloseRequest
	if !httptransport.DecodeJSON(w, r, &input, 8192) {
		return
	}
	if input.IdleMinutes == nil || !capacity.ValidIdleMinutes(*input.IdleMinutes) || len(input.Sessions) == 0 || len(input.Sessions) > capacity.Limit || (*input.IdleMinutes == 0 && len(input.Sessions) != 1) {
		httptransport.WriteError(w, http.StatusBadRequest, "invalid console cleanup selection")
		return
	}
	seen := map[int64]bool{}
	for _, item := range input.Sessions {
		if item.ID < 1 || item.Generation < 1 || item.LastActivity == "" || seen[item.ID] {
			httptransport.WriteError(w, http.StatusBadRequest, "invalid console cleanup selection")
			return
		}
		seen[item.ID] = true
	}
	result := capacity.CloseResult{Closed: []int64{}, Skipped: []int64{}, Uncertain: []int64{}}
	for _, item := range input.Sessions {
		err := sessions.CloseCapacitySession(r.Context(), principal, item, *input.IdleMinutes)
		switch {
		case err == nil:
			result.Closed = append(result.Closed, item.ID)
		case errors.Is(err, ErrLiveConsoleNotFound), errors.Is(err, ErrLiveConsoleChanged):
			result.Skipped = append(result.Skipped, item.ID)
		default:
			result.Uncertain = append(result.Uncertain, item.ID)
		}
	}
	auditCtx, cancelAudit := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancelAudit()
	runtime.observe(auditCtx, 0, "console.capacity.cleanup", map[string]any{"closed_session_ids": result.Closed, "skipped_session_ids": result.Skipped, "uncertain_session_ids": result.Uncertain, "idle_minutes": *input.IdleMinutes})
	httptransport.WriteJSON(w, http.StatusOK, result)
}

var ErrLiveConsoleChanged = errors.New("console session changed")
