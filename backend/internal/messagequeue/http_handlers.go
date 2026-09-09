package messagequeue

import (
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type Scope struct {
	Store *Store
}

type ScopeProvider func(http.ResponseWriter) (Scope, bool)

type Handlers struct {
	scope ScopeProvider
}

func NewHTTPHandlers(scope ScopeProvider) *Handlers {
	return &Handlers{scope: scope}
}

type MarkReadRequest struct {
	RuntimeID int64 `json:"runtime_id"`
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store(w)
	if !ok {
		return
	}
	filter := Filter{Direction: strings.TrimSpace(r.URL.Query().Get("direction"))}
	if raw := strings.TrimSpace(r.URL.Query().Get("runtime_id")); raw != "" {
		id, valid := httptransport.ParseQueryInt64(w, raw, "runtime_id")
		if !valid {
			return
		}
		filter.RuntimeID = id
	}
	items, err := store.List(r.Context(), filter)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, items)
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store(w)
	if !ok {
		return
	}
	var request CreateRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	item, err := store.Insert(r.Context(), request)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handlers) MarkRead(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store(w)
	if !ok {
		return
	}
	var request MarkReadRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if request.RuntimeID < 1 {
		httptransport.WriteError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	count, err := store.MarkRuntimeRead(r.Context(), request.RuntimeID)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"status": "read", "count": count})
}

func (h *Handlers) store(w http.ResponseWriter) (*Store, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return nil, false
	}
	if scope.Store == nil || scope.Store.database == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return scope.Store, true
}
