package commandrequests

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type HTTPScopeProvider func(http.ResponseWriter) (*Store, bool)

type HTTPHandlers struct {
	scope HTTPScopeProvider
}

func NewHTTPHandlers(scope HTTPScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope}
}

func (h *HTTPHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return
	}
	store, ok := h.scope(w)
	if !ok {
		return
	}
	if store == nil {
		httptransport.WriteInternalError(w)
		return
	}
	item, err := store.Get(r.Context(), id, 0, "")
	if errors.Is(err, sql.ErrNoRows) {
		httptransport.WriteError(w, http.StatusNotFound, "command request not found")
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}
