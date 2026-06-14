package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type historyEntryFilter struct {
	ConnectorKind string
	ActivityType  string
	Status        string
	Source        string
	RuntimeID     int64
	TargetID      int64
	ProfileID     int64
	LabelID       int64
	Query         string
	Limit         int
	Offset        int
}

type historyEntryRecord struct {
	ID               int64                `json:"id"`
	SourceRefType    string               `json:"source_ref_type"`
	SourceRefID      int64                `json:"source_ref_id"`
	ConnectorKind    string               `json:"connector_kind"`
	ActivityType     string               `json:"activity_type"`
	TokenID          *int64               `json:"token_id,omitempty"`
	TokenName        string               `json:"token_name,omitempty"`
	RuntimeID        *int64               `json:"runtime_id,omitempty"`
	TargetID         *int64               `json:"target_id,omitempty"`
	ProfileID        *int64               `json:"profile_id,omitempty"`
	TargetName       string               `json:"target_name"`
	ProfileLabel     string               `json:"profile_label,omitempty"`
	Source           string               `json:"source"`
	Status           string               `json:"status"`
	ActionName       string               `json:"action_name"`
	Title            string               `json:"title"`
	Summary          string               `json:"summary"`
	PreviewJSON      string               `json:"preview_json,omitempty"`
	InputText        string               `json:"input_text,omitempty"`
	InputJSON        string               `json:"input_json,omitempty"`
	OutputText       string               `json:"output_text,omitempty"`
	OutputJSON       string               `json:"output_json,omitempty"`
	Error            string               `json:"error,omitempty"`
	ExitCode         *int                 `json:"exit_code,omitempty"`
	ProgressCurrent  int64                `json:"progress_current"`
	ProgressTotal    int64                `json:"progress_total"`
	BytesDone        int64                `json:"bytes_done"`
	BytesTotal       int64                `json:"bytes_total"`
	ApprovalRequired bool                 `json:"approval_required"`
	UserNote         string               `json:"user_note,omitempty"`
	CreatedAt        string               `json:"created_at"`
	StartedAt        *string              `json:"started_at,omitempty"`
	CompletedAt      *string              `json:"completed_at,omitempty"`
	UpdatedAt        string               `json:"updated_at"`
	Labels           []historyLabelRecord `json:"labels"`
}

type historyTargetFacetRecord struct {
	Ref           string `json:"ref"`
	ConnectorKind string `json:"connector_kind"`
	RuntimeID     *int64 `json:"runtime_id,omitempty"`
	TargetID      *int64 `json:"target_id,omitempty"`
	ProfileID     *int64 `json:"profile_id,omitempty"`
	TargetName    string `json:"target_name"`
	ProfileLabel  string `json:"profile_label,omitempty"`
	LastSeenAt    string `json:"last_seen_at"`
}

func (s historyEntryHandlers) listHistoryTargetFacets(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	items, err := s.listHistoryTargets(r.Context(), runtime)
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
	page, err := parsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := historyEntryFilter{
		ConnectorKind: strings.TrimSpace(r.URL.Query().Get("connector_kind")),
		ActivityType:  strings.TrimSpace(r.URL.Query().Get("activity_type")),
		Status:        strings.TrimSpace(r.URL.Query().Get("status")),
		Source:        strings.TrimSpace(r.URL.Query().Get("source")),
		Query:         page.Query,
		Limit:         page.Limit,
		Offset:        page.Offset,
	}
	if rawRuntimeID := strings.TrimSpace(r.URL.Query().Get("runtime_id")); rawRuntimeID != "" {
		id, ok := parseInt64Query(w, rawRuntimeID, "runtime_id")
		if !ok {
			return
		}
		filter.RuntimeID = id
	}
	if rawTargetID := strings.TrimSpace(r.URL.Query().Get("target_id")); rawTargetID != "" {
		id, ok := parseInt64Query(w, rawTargetID, "target_id")
		if !ok {
			return
		}
		filter.TargetID = id
	}
	if rawProfileID := strings.TrimSpace(r.URL.Query().Get("profile_id")); rawProfileID != "" {
		id, ok := parseInt64Query(w, rawProfileID, "profile_id")
		if !ok {
			return
		}
		filter.ProfileID = id
	}
	if rawLabelID := strings.TrimSpace(r.URL.Query().Get("label_id")); rawLabelID != "" {
		id, ok := parseInt64Query(w, rawLabelID, "label_id")
		if !ok {
			return
		}
		filter.LabelID = id
	}
	items, total, err := s.listHistoryEntrySummaries(r.Context(), runtime, filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, makePageResponse(items, total, page))
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
	item, err := s.getHistoryEntryRecord(r.Context(), runtime, id)
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

func (s *Server) listHistoryTargets(ctx context.Context, runtime *databaseRuntime) ([]historyTargetFacetRecord, error) {
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT
			he.connector_kind,
			he.runtime_id,
			he.target_id,
			he.profile_id,
			COALESCE(NULLIF(he.target_name, ''), 'Unknown connector') AS target_name,
			COALESCE(he.profile_label, '') AS profile_label,
			MAX(he.created_at) AS last_seen_at
		FROM history_entries he
		WHERE he.target_id IS NOT NULL OR he.runtime_id IS NOT NULL
		GROUP BY he.connector_kind, he.runtime_id, he.target_id, he.profile_id, he.target_name, he.profile_label
		ORDER BY lower(target_name), lower(profile_label), he.connector_kind`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []historyTargetFacetRecord{}
	for rows.Next() {
		var item historyTargetFacetRecord
		var runtimeID sql.NullInt64
		var targetID sql.NullInt64
		var profileID sql.NullInt64
		if err := rows.Scan(
			&item.ConnectorKind,
			&runtimeID,
			&targetID,
			&profileID,
			&item.TargetName,
			&item.ProfileLabel,
			&item.LastSeenAt,
		); err != nil {
			return nil, err
		}
		if runtimeID.Valid {
			value := runtimeID.Int64
			item.RuntimeID = &value
		}
		if targetID.Valid {
			value := targetID.Int64
			item.TargetID = &value
		}
		if profileID.Valid {
			value := profileID.Int64
			item.ProfileID = &value
		}
		if targetID.Valid && profileID.Valid {
			item.Ref = connectortargets.ConnectorTargetRef(item.ConnectorKind, targetID.Int64, profileID.Int64)
		} else if runtimeID.Valid {
			item.Ref = "runtime:" + strconv.FormatInt(runtimeID.Int64, 10)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) listHistoryEntrySummaries(ctx context.Context, runtime *databaseRuntime, filter historyEntryFilter) ([]historyEntryRecord, int, error) {
	where, args := historyEntryWhere(filter)
	var total int
	if err := runtime.database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM history_entries he
		LEFT JOIN api_tokens tok ON tok.id = he.token_id
		WHERE `+where,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT he.id, he.source_ref_type, he.source_ref_id, he.connector_kind, he.activity_type,
		       he.token_id, COALESCE(tok.name, ''), he.runtime_id, he.target_id, he.profile_id,
		       he.target_name, he.profile_label, he.source, he.status, he.action_name,
		       he.title, he.summary, he.preview_json, he.input_text, he.input_json, '' AS output_text,
		       '{}' AS output_json, he.error, he.exit_code, he.progress_current,
		       he.progress_total, he.bytes_done, he.bytes_total, he.approval_required,
		       he.user_note, he.created_at, he.started_at, he.completed_at, he.updated_at
		FROM history_entries he
		LEFT JOIN api_tokens tok ON tok.id = he.token_id
		WHERE `+where+`
		ORDER BY he.created_at DESC, he.id DESC
		LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []historyEntryRecord{}
	for rows.Next() {
		item, err := scanHistoryEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := s.attachLabelsToHistoryEntries(ctx, runtime, items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Server) getHistoryEntryRecord(ctx context.Context, runtime *databaseRuntime, id int64) (historyEntryRecord, error) {
	row := runtime.database.QueryRowContext(ctx, `
		SELECT he.id, he.source_ref_type, he.source_ref_id, he.connector_kind, he.activity_type,
		       he.token_id, COALESCE(tok.name, ''), he.runtime_id, he.target_id, he.profile_id,
		       he.target_name, he.profile_label, he.source, he.status, he.action_name,
		       he.title, he.summary, he.preview_json, he.input_text, he.input_json, he.output_text,
		       he.output_json, he.error, he.exit_code, he.progress_current,
		       he.progress_total, he.bytes_done, he.bytes_total, he.approval_required,
		       he.user_note, he.created_at, he.started_at, he.completed_at, he.updated_at
		FROM history_entries he
		LEFT JOIN api_tokens tok ON tok.id = he.token_id
		WHERE he.id = ?`,
		id,
	)
	item, err := scanHistoryEntry(row)
	if err != nil {
		return historyEntryRecord{}, err
	}
	labels, err := s.labelsForHistoryEntry(ctx, runtime, item.ID)
	if err != nil {
		return historyEntryRecord{}, err
	}
	item.Labels = labels
	return item, nil
}

func historyEntryWhere(filter historyEntryFilter) (string, []any) {
	where := []string{
		"(? = '' OR he.connector_kind = ?)",
		"(? = '' OR he.activity_type = ?)",
		"(? = '' OR he.status = ?)",
		"(? = '' OR he.source = ?)",
		"(? = 0 OR he.runtime_id = ?)",
		"(? = 0 OR he.target_id = ? OR he.runtime_id IN (SELECT id FROM connector_runtime_surfaces WHERE target_id = ? AND status = 'active'))",
		"(? = 0 OR he.profile_id = ? OR he.runtime_id IN (SELECT id FROM connector_runtime_surfaces WHERE profile_id = ? AND status = 'active'))",
	}
	args := []any{
		filter.ConnectorKind, filter.ConnectorKind,
		filter.ActivityType, filter.ActivityType,
		filter.Status, filter.Status,
		filter.Source, filter.Source,
		filter.RuntimeID, filter.RuntimeID,
		filter.TargetID, filter.TargetID, filter.TargetID,
		filter.ProfileID, filter.ProfileID, filter.ProfileID,
	}
	if filter.LabelID != 0 {
		where = append(where, `he.id IN (SELECT history_entry_id FROM history_entry_labels WHERE label_id = ?)`)
		args = append(args, filter.LabelID)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		where = append(where, `(he.title LIKE ? OR he.summary LIKE ? OR he.preview_json LIKE ? OR he.input_text LIKE ? OR he.input_json LIKE ? OR he.output_text LIKE ? OR he.output_json LIKE ? OR he.error LIKE ? OR he.target_name LIKE ? OR he.profile_label LIKE ? OR he.action_name LIKE ? OR COALESCE(tok.name, '') LIKE ?)`)
		args = append(args, like, like, like, like, like, like, like, like, like, like, like, like)
	}
	return strings.Join(where, " AND "), args
}

func scanHistoryEntry(scanner interface {
	Scan(dest ...any) error
}) (historyEntryRecord, error) {
	var item historyEntryRecord
	var tokenID sql.NullInt64
	var runtimeID sql.NullInt64
	var targetID sql.NullInt64
	var profileID sql.NullInt64
	var exitCode sql.NullInt64
	var startedAt sql.NullString
	var completedAt sql.NullString
	var approvalRequired int
	err := scanner.Scan(
		&item.ID,
		&item.SourceRefType,
		&item.SourceRefID,
		&item.ConnectorKind,
		&item.ActivityType,
		&tokenID,
		&item.TokenName,
		&runtimeID,
		&targetID,
		&profileID,
		&item.TargetName,
		&item.ProfileLabel,
		&item.Source,
		&item.Status,
		&item.ActionName,
		&item.Title,
		&item.Summary,
		&item.PreviewJSON,
		&item.InputText,
		&item.InputJSON,
		&item.OutputText,
		&item.OutputJSON,
		&item.Error,
		&exitCode,
		&item.ProgressCurrent,
		&item.ProgressTotal,
		&item.BytesDone,
		&item.BytesTotal,
		&approvalRequired,
		&item.UserNote,
		&item.CreatedAt,
		&startedAt,
		&completedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return historyEntryRecord{}, err
	}
	if tokenID.Valid {
		value := tokenID.Int64
		item.TokenID = &value
	}
	if runtimeID.Valid {
		value := runtimeID.Int64
		item.RuntimeID = &value
	}
	if targetID.Valid {
		value := targetID.Int64
		item.TargetID = &value
	}
	if profileID.Valid {
		value := profileID.Int64
		item.ProfileID = &value
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		item.ExitCode = &value
	}
	if startedAt.Valid {
		item.StartedAt = stringPtr(startedAt.String)
	}
	if completedAt.Valid {
		item.CompletedAt = stringPtr(completedAt.String)
	}
	item.ApprovalRequired = approvalRequired != 0
	item.Labels = []historyLabelRecord{}
	return item, nil
}

func (s *Server) labelsForHistoryEntry(ctx context.Context, runtime *databaseRuntime, entryID int64) ([]historyLabelRecord, error) {
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT hl.id, hl.name, hl.color, hl.created_at, hl.updated_at
		FROM history_labels hl
		JOIN history_entry_labels hel ON hel.label_id = hl.id
		WHERE hel.history_entry_id = ?
		ORDER BY lower(hl.name), hl.id`,
		entryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []historyLabelRecord{}
	for rows.Next() {
		var label historyLabelRecord
		if err := rows.Scan(&label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func (s *Server) attachLabelsToHistoryEntries(ctx context.Context, runtime *databaseRuntime, items []historyEntryRecord) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	args := make([]any, 0, len(items))
	byID := map[int64]int{}
	for index := range items {
		items[index].Labels = []historyLabelRecord{}
		ids = append(ids, "?")
		args = append(args, items[index].ID)
		byID[items[index].ID] = index
	}
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT hel.history_entry_id, hl.id, hl.name, hl.color, hl.created_at, hl.updated_at
		FROM history_entry_labels hel
		JOIN history_labels hl ON hl.id = hel.label_id
		WHERE hel.history_entry_id IN (`+strings.Join(ids, ",")+`)
		ORDER BY lower(hl.name), hl.id`,
		args...,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var entryID int64
		var label historyLabelRecord
		if err := rows.Scan(&entryID, &label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt); err != nil {
			return err
		}
		if index, ok := byID[entryID]; ok {
			items[index].Labels = append(items[index].Labels, label)
		}
	}
	return rows.Err()
}
