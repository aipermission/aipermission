package observability

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const (
	defaultHTTPPageLimit = 50
	maxHTTPPageLimit     = 100
)

type HTTPScope struct {
	Database *sql.DB
}

type HTTPScopeProvider func(http.ResponseWriter) (HTTPScope, bool)

type HTTPHandlers struct {
	scope HTTPScopeProvider
}

func NewHTTPHandlers(scope HTTPScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope}
}

type httpPage struct {
	Limit  int
	Offset int
	Query  string
}

type pageResponse struct {
	Items      []Record `json:"items"`
	Total      int      `json:"total"`
	Limit      int      `json:"limit"`
	Offset     int      `json:"offset"`
	NextOffset *int     `json:"next_offset,omitempty"`
}

func (h *HTTPHandlers) List(w http.ResponseWriter, r *http.Request) {
	database, ok := h.database(w)
	if !ok {
		return
	}
	page, err := parseHTTPPage(r)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := QueryFilter{
		Actor:         strings.TrimSpace(r.URL.Query().Get("actor")),
		ConnectorKind: strings.TrimSpace(r.URL.Query().Get("connector_kind")),
		Query:         page.Query,
		Limit:         page.Limit,
		Offset:        page.Offset,
	}
	for _, value := range []struct {
		name string
		set  func(int64)
	}{
		{"runtime_id", func(id int64) { filter.RuntimeID = id }},
		{"project_id", func(id int64) { filter.ProjectID = id }},
		{"target_id", func(id int64) { filter.TargetID = id }},
	} {
		raw := strings.TrimSpace(r.URL.Query().Get(value.name))
		if raw == "" {
			continue
		}
		id, valid := httptransport.ParseQueryInt64(w, raw, value.name)
		if !valid {
			return
		}
		value.set(id)
	}
	result, err := NewQueryStore(database).List(r.Context(), filter)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	response := pageResponse{Items: result.Items, Total: result.Total, Limit: page.Limit, Offset: page.Offset}
	if page.Offset+len(result.Items) < result.Total {
		next := page.Offset + len(result.Items)
		response.NextOffset = &next
	}
	httptransport.WriteJSON(w, http.StatusOK, response)
}

func (h *HTTPHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	database, ok := h.database(w)
	if !ok {
		return
	}
	item, err := NewQueryStore(database).Get(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		httptransport.WriteError(w, http.StatusNotFound, "audit log not found")
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *HTTPHandlers) database(w http.ResponseWriter) (*sql.DB, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return nil, false
	}
	if scope.Database == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return scope.Database, true
}

func parseHTTPPage(r *http.Request) (httpPage, error) {
	page := httpPage{Limit: defaultHTTPPageLimit, Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return httpPage{}, errors.New("invalid limit")
		}
		page.Limit = min(limit, maxHTTPPageLimit)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return httpPage{}, errors.New("invalid offset")
		}
		page.Offset = offset
	}
	return page, nil
}
