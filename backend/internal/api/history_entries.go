package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/history"
)

type historyEntryRecord = history.Entry
type historyTargetFacetRecord = history.TargetFacet

func (s historyEntryHandlers) listHistoryTargetFacets(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	items, err := history.NewQueryStore(runtime.database).Targets(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s historyEntryHandlers) listHistoryEntries(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	page, err := parseHistoryPageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := history.QueryFilter{
		ConnectorKind: strings.TrimSpace(r.URL.Query().Get("connector_kind")),
		ActivityType:  strings.TrimSpace(r.URL.Query().Get("activity_type")),
		Status:        strings.TrimSpace(r.URL.Query().Get("status")),
		Source:        strings.TrimSpace(r.URL.Query().Get("source")),
		Query:         page.Query,
		Limit:         page.Limit,
	}
	if page.Cursor != nil {
		filter.BeforeTime = page.Cursor.CreatedAt
		filter.BeforeID = page.Cursor.ID
	}
	queryIDs := []struct {
		raw   string
		label string
		set   func(int64)
	}{
		{r.URL.Query().Get("runtime_id"), "runtime_id", func(id int64) { filter.RuntimeID = id }},
		{r.URL.Query().Get("project_id"), "project_id", func(id int64) { filter.ProjectID = id }},
		{r.URL.Query().Get("target_id"), "target_id", func(id int64) { filter.TargetID = id }},
		{r.URL.Query().Get("profile_id"), "profile_id", func(id int64) { filter.ProfileID = id }},
		{r.URL.Query().Get("label_id"), "label_id", func(id int64) { filter.LabelID = id }},
	}
	for _, field := range queryIDs {
		if strings.TrimSpace(field.raw) == "" {
			continue
		}
		id, valid := parseInt64Query(w, field.raw, field.label)
		if !valid {
			return
		}
		field.set(id)
	}
	store := history.NewQueryStore(runtime.database)
	var total *int
	if page.IncludeTotal {
		countFilter := filter
		countFilter.BeforeTime, countFilter.BeforeID = "", 0
		count, err := store.Count(r.Context(), countFilter)
		if err != nil {
			writeInternalError(w)
			return
		}
		total = &count
	}
	items, hasMore, err := store.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	response := historyPageResponse{Items: items, Total: total, Limit: page.Limit, HasMore: hasMore}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		response.NextCursor = encodeHistoryCursor(historyCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s historyEntryHandlers) getHistoryEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	item, err := history.NewQueryStore(runtime.database).Get(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "history entry not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
