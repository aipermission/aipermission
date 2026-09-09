package history

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const (
	defaultPageLimit       = 50
	maxPageLimit           = 100
	maxHistoryCursorLength = 256
)

var ErrMutationUnchanged = errors.New("history mutation unchanged")

type Scope struct {
	Database *sql.DB
	Mutate   auditedmutation.Runner
}

type ScopeProvider func(http.ResponseWriter) (Scope, bool)

type Handlers struct {
	scope ScopeProvider
}

func New(scope ScopeProvider) *Handlers {
	return &Handlers{scope: scope}
}

type CreateLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type AttachLabelRequest struct {
	LabelID int64  `json:"label_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Color   string `json:"color,omitempty"`
}

type PageResponse struct {
	Items      []Entry `json:"items"`
	Total      *int    `json:"total,omitempty"`
	Limit      int     `json:"limit"`
	NextCursor string  `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
}

func (h *Handlers) ListTargetFacets(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w)
	if !ok {
		return
	}
	items, err := NewQueryStore(scope.Database).Targets(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handlers) ListEntries(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w)
	if !ok {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := QueryFilter{
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
		id, valid := httptransport.ParseQueryInt64(w, field.raw, field.label)
		if !valid {
			return
		}
		field.set(id)
	}
	store := NewQueryStore(scope.Database)
	var total *int
	if page.IncludeTotal {
		countFilter := filter
		countFilter.BeforeTime, countFilter.BeforeID = "", 0
		count, err := store.Count(r.Context(), countFilter)
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		total = &count
	}
	items, hasMore, err := store.List(r.Context(), filter)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	response := PageResponse{Items: items, Total: total, Limit: page.Limit, HasMore: hasMore}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		response.NextCursor = encodeCursor(cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	httptransport.WriteJSON(w, http.StatusOK, response)
}

func (h *Handlers) GetEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	scope, ok := h.runtimeScope(w)
	if !ok {
		return
	}
	item, err := NewQueryStore(scope.Database).Get(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		httptransport.WriteError(w, http.StatusNotFound, "history entry not found")
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *Handlers) ListLabels(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w)
	if !ok {
		return
	}
	labels, err := NewLabelStore(scope.Database).All(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, labels)
}

func (h *Handlers) CreateLabel(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w)
	if !ok || !h.requireMutation(w, scope) {
		return
	}
	var request CreateLabelRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	var label Label
	var created bool
	err := scope.Mutate(r.Context(), "history.label.created",
		func() any { return map[string]any{"label_id": label.ID, "name": label.Name} },
		func(tx *sql.Tx) error {
			var err error
			label, created, err = NewLabelStore(tx).CreateOrGet(r.Context(), request.Name, request.Color)
			if err == nil && !created {
				return ErrMutationUnchanged
			}
			return err
		},
	)
	if errors.Is(err, ErrMutationUnchanged) {
		label, err = NewLabelStore(scope.Database).GetByName(r.Context(), request.Name)
	}
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httptransport.WriteJSON(w, status, label)
}

func (h *Handlers) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w)
	if !ok || !h.requireMutation(w, scope) {
		return
	}
	labelID, ok := httptransport.ParsePathInt64(w, r, "id", "label id is required")
	if !ok {
		return
	}
	err := scope.Mutate(r.Context(), "history.label.deleted",
		func() any { return map[string]any{"label_id": labelID} },
		func(tx *sql.Tx) error { return NewLabelStore(tx).Delete(r.Context(), labelID) },
	)
	if errors.Is(err, sql.ErrNoRows) {
		httptransport.WriteError(w, http.StatusNotFound, "history label not found")
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handlers) AttachEntryLabel(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	scope, ok := h.runtimeScope(w)
	if !ok || !h.requireMutation(w, scope) {
		return
	}
	var request AttachLabelRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	exists, err := NewLabelStore(scope.Database).EntryExists(r.Context(), id)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if !exists {
		httptransport.WriteError(w, http.StatusNotFound, "history entry not found")
		return
	}
	var label Label
	var created bool
	err = scope.Mutate(r.Context(), "history.label.attached",
		func() any { return map[string]any{"history_entry_id": id, "label_id": label.ID, "created": created} },
		func(tx *sql.Tx) error {
			store := NewLabelStore(tx)
			var err error
			if request.LabelID > 0 {
				label, err = store.Get(r.Context(), request.LabelID)
			} else {
				label, created, err = store.CreateOrGet(r.Context(), request.Name, request.Color)
			}
			if err != nil {
				return err
			}
			attached, err := store.Attach(r.Context(), id, label.ID)
			if err == nil && !attached && !created {
				return ErrMutationUnchanged
			}
			return err
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		httptransport.WriteError(w, http.StatusNotFound, "label not found")
		return
	}
	if errors.Is(err, ErrMutationUnchanged) {
		err = nil
	}
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	labels, err := NewQueryStore(scope.Database).Labels(r.Context(), id)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httptransport.WriteJSON(w, status, labels)
}

func (h *Handlers) DetachEntryLabel(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	labelID, ok := httptransport.ParsePathInt64(w, r, "label_id", "label id is required")
	if !ok {
		return
	}
	scope, ok := h.runtimeScope(w)
	if !ok || !h.requireMutation(w, scope) {
		return
	}
	exists, err := NewLabelStore(scope.Database).EntryExists(r.Context(), id)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if !exists {
		httptransport.WriteError(w, http.StatusNotFound, "history entry not found")
		return
	}
	err = scope.Mutate(r.Context(), "history.label.detached",
		func() any { return map[string]any{"history_entry_id": id, "label_id": labelID} },
		func(tx *sql.Tx) error { return NewLabelStore(tx).Detach(r.Context(), id, labelID) },
	)
	if errors.Is(err, sql.ErrNoRows) {
		httptransport.WriteError(w, http.StatusNotFound, "history label relationship not found")
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	labels, err := NewQueryStore(scope.Database).Labels(r.Context(), id)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, labels)
}

func (h *Handlers) runtimeScope(w http.ResponseWriter) (Scope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return Scope{}, false
	}
	if scope.Database == nil {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	return scope, true
}

func (h *Handlers) requireMutation(w http.ResponseWriter, scope Scope) bool {
	if scope.Mutate != nil {
		return true
	}
	httptransport.WriteInternalError(w)
	return false
}

type cursor struct {
	CreatedAt string
	ID        int64
}

type pageRequest struct {
	Limit        int
	Query        string
	Cursor       *cursor
	IncludeTotal bool
}

func parsePageRequest(r *http.Request) (pageRequest, error) {
	if _, present := r.URL.Query()["offset"]; present {
		return pageRequest{}, errors.New("history offset pagination is not supported; use cursor")
	}
	limit, err := parsePageLimit(r)
	if err != nil {
		return pageRequest{}, err
	}
	decodedCursor, err := decodeCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		return pageRequest{}, err
	}
	includeTotal := decodedCursor == nil
	if raw := strings.TrimSpace(r.URL.Query().Get("include_total")); raw != "" {
		includeTotal, err = strconv.ParseBool(raw)
		if err != nil {
			return pageRequest{}, errors.New("invalid include_total")
		}
	}
	return pageRequest{
		Limit: limit, Query: strings.TrimSpace(r.URL.Query().Get("q")),
		Cursor: decodedCursor, IncludeTotal: includeTotal,
	}, nil
}

func parsePageLimit(r *http.Request) (int, error) {
	limit := defaultPageLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return 0, errors.New("invalid limit")
		}
		limit = value
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return limit, nil
}

func encodeCursor(value cursor) string {
	payload := value.CreatedAt + "\n" + strconv.FormatInt(value.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(raw string) (*cursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxHistoryCursorLength {
		return nil, errors.New("invalid history cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid history cursor")
	}
	parts := strings.Split(string(payload), "\n")
	if len(parts) != 2 || parts[0] == "" || len(parts[0]) > 64 {
		return nil, errors.New("invalid history cursor")
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id < 1 {
		return nil, errors.New("invalid history cursor")
	}
	return &cursor{CreatedAt: parts[0], ID: id}, nil
}
