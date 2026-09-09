package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type QueryFilter struct {
	ProjectID     int64
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
	BeforeTime    string
	BeforeID      int64
}

type Label struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type Entry struct {
	ID               int64   `json:"id"`
	SourceRefType    string  `json:"source_ref_type"`
	SourceRefID      int64   `json:"source_ref_id"`
	ConnectorKind    string  `json:"connector_kind"`
	ActivityType     string  `json:"activity_type"`
	TokenID          *int64  `json:"token_id,omitempty"`
	TokenName        string  `json:"token_name,omitempty"`
	ProjectID        *int64  `json:"project_id,omitempty"`
	ProjectName      string  `json:"project_name,omitempty"`
	RuntimeID        *int64  `json:"runtime_id,omitempty"`
	TargetID         *int64  `json:"target_id,omitempty"`
	ProfileID        *int64  `json:"profile_id,omitempty"`
	TargetName       string  `json:"target_name"`
	ProfileLabel     string  `json:"profile_label,omitempty"`
	Source           string  `json:"source"`
	Status           string  `json:"status"`
	ActionName       string  `json:"action_name"`
	Title            string  `json:"title"`
	Summary          string  `json:"summary"`
	PreviewJSON      string  `json:"preview_json,omitempty"`
	InputText        string  `json:"input_text,omitempty"`
	InputJSON        string  `json:"input_json,omitempty"`
	OutputText       string  `json:"output_text,omitempty"`
	OutputJSON       string  `json:"output_json,omitempty"`
	Error            string  `json:"error,omitempty"`
	RetryPolicyJSON  string  `json:"retry_policy_json,omitempty"`
	ExitCode         *int    `json:"exit_code,omitempty"`
	ProgressCurrent  int64   `json:"progress_current"`
	ProgressTotal    int64   `json:"progress_total"`
	BytesDone        int64   `json:"bytes_done"`
	BytesTotal       int64   `json:"bytes_total"`
	ApprovalRequired bool    `json:"approval_required"`
	UserNote         string  `json:"user_note,omitempty"`
	CreatedAt        string  `json:"created_at"`
	StartedAt        *string `json:"started_at,omitempty"`
	CompletedAt      *string `json:"completed_at,omitempty"`
	UpdatedAt        string  `json:"updated_at"`
	Labels           []Label `json:"labels"`
}

type TargetFacet struct {
	Ref           string `json:"ref"`
	ProjectID     *int64 `json:"project_id,omitempty"`
	ProjectName   string `json:"project_name,omitempty"`
	ConnectorKind string `json:"connector_kind"`
	RuntimeID     *int64 `json:"runtime_id,omitempty"`
	TargetID      *int64 `json:"target_id,omitempty"`
	ProfileID     *int64 `json:"profile_id,omitempty"`
	TargetName    string `json:"target_name"`
	ProfileLabel  string `json:"profile_label,omitempty"`
	LastSeenAt    string `json:"last_seen_at"`
}

type QueryStore struct{ database *sql.DB }

func NewQueryStore(database *sql.DB) *QueryStore { return &QueryStore{database: database} }

func (s *QueryStore) Targets(ctx context.Context) ([]TargetFacet, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT he.connector_kind, he.project_id, COALESCE(project.name, '') AS project_name,
			he.runtime_id, he.target_id, he.profile_id,
			COALESCE(NULLIF(he.target_name, ''), 'Unknown connector') AS target_name,
			COALESCE(he.profile_label, '') AS profile_label, MAX(he.created_at) AS last_seen_at
		FROM history_entries he
		LEFT JOIN projects project ON project.id = he.project_id
		WHERE he.target_id IS NOT NULL OR he.runtime_id IS NOT NULL
		GROUP BY he.connector_kind, he.project_id, project.name, he.runtime_id,
			he.target_id, he.profile_id, he.target_name, he.profile_label
		ORDER BY lower(project_name), lower(target_name), lower(profile_label), he.connector_kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TargetFacet{}
	for rows.Next() {
		var item TargetFacet
		var projectID, runtimeID, targetID, profileID sql.NullInt64
		if err := rows.Scan(&item.ConnectorKind, &projectID, &item.ProjectName, &runtimeID,
			&targetID, &profileID, &item.TargetName, &item.ProfileLabel, &item.LastSeenAt); err != nil {
			return nil, err
		}
		item.ProjectID = nullableInt64(projectID)
		item.RuntimeID = nullableInt64(runtimeID)
		item.TargetID = nullableInt64(targetID)
		item.ProfileID = nullableInt64(profileID)
		if targetID.Valid && profileID.Valid {
			item.Ref = fmt.Sprintf("%s:%d:%d", item.ConnectorKind, targetID.Int64, profileID.Int64)
		} else if runtimeID.Valid {
			item.Ref = "runtime:" + strconv.FormatInt(runtimeID.Int64, 10)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *QueryStore) Count(ctx context.Context, filter QueryFilter) (int, error) {
	where, args := queryWhere(filter)
	joins := ""
	if filter.Query != "" {
		joins = ` LEFT JOIN api_tokens tok ON tok.id = he.token_id LEFT JOIN projects project ON project.id = he.project_id`
	}
	var total int
	err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_entries he`+joins+` WHERE `+where, args...).Scan(&total)
	return total, err
}

func (s *QueryStore) List(ctx context.Context, filter QueryFilter) ([]Entry, bool, error) {
	where, args := queryWhere(filter)
	queryArgs := append(append([]any{}, args...), filter.Limit+1)
	rows, err := s.database.QueryContext(ctx, `
		SELECT he.id, he.source_ref_type, he.source_ref_id, he.connector_kind, he.activity_type,
		       he.token_id, COALESCE(tok.name, ''), he.project_id, COALESCE(project.name, ''), he.runtime_id, he.target_id, he.profile_id,
		       he.target_name, he.profile_label, he.source, he.status, he.action_name,
		       he.title, he.summary, he.preview_json, he.input_text, he.input_json, '' AS output_text,
		       '{}' AS output_json, he.error, he.retry_policy_json, he.exit_code, he.progress_current,
		       he.progress_total, he.bytes_done, he.bytes_total, he.approval_required,
		       he.user_note, he.created_at, he.started_at, he.completed_at, he.updated_at
		FROM history_entries he
		LEFT JOIN api_tokens tok ON tok.id = he.token_id
		LEFT JOIN projects project ON project.id = he.project_id
		WHERE `+where+`
		ORDER BY he.created_at DESC, he.id DESC
		LIMIT ?`, queryArgs...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := []Entry{}
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > filter.Limit
	if hasMore {
		items = items[:filter.Limit]
	}
	if err := s.attachLabels(ctx, items); err != nil {
		return nil, false, err
	}
	return items, hasMore, nil
}

func (s *QueryStore) Get(ctx context.Context, id int64) (Entry, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT he.id, he.source_ref_type, he.source_ref_id, he.connector_kind, he.activity_type,
		       he.token_id, COALESCE(tok.name, ''), he.project_id, COALESCE(project.name, ''), he.runtime_id, he.target_id, he.profile_id,
		       he.target_name, he.profile_label, he.source, he.status, he.action_name,
		       he.title, he.summary, he.preview_json, he.input_text, he.input_json, he.output_text,
		       he.output_json, he.error, he.retry_policy_json, he.exit_code, he.progress_current,
		       he.progress_total, he.bytes_done, he.bytes_total, he.approval_required,
		       he.user_note, he.created_at, he.started_at, he.completed_at, he.updated_at
		FROM history_entries he
		LEFT JOIN api_tokens tok ON tok.id = he.token_id
		LEFT JOIN projects project ON project.id = he.project_id
		WHERE he.id = ?`, id)
	item, err := scanEntry(row)
	if err != nil {
		return Entry{}, err
	}
	item.Labels, err = s.Labels(ctx, item.ID)
	return item, err
}

func (s *QueryStore) Labels(ctx context.Context, entryID int64) ([]Label, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT hl.id, hl.name, hl.color, hl.created_at, hl.updated_at
		FROM history_labels hl JOIN history_entry_labels hel ON hel.label_id = hl.id
		WHERE hel.history_entry_id = ? ORDER BY lower(hl.name), hl.id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []Label{}
	for rows.Next() {
		var label Label
		if err := rows.Scan(&label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func queryWhere(filter QueryFilter) (string, []any) {
	where, args := []string{"1 = 1"}, []any{}
	add := func(clause string, values ...any) { where = append(where, clause); args = append(args, values...) }
	if filter.ProjectID != 0 {
		add("he.project_id = ?", filter.ProjectID)
	}
	if filter.ConnectorKind != "" {
		add("he.connector_kind = ?", filter.ConnectorKind)
	}
	if filter.ActivityType != "" {
		add("he.activity_type = ?", filter.ActivityType)
	}
	if filter.Status != "" {
		add("he.status = ?", filter.Status)
	}
	if filter.Source != "" {
		add("he.source = ?", filter.Source)
	}
	if filter.RuntimeID != 0 {
		add("he.runtime_id = ?", filter.RuntimeID)
	}
	if filter.TargetID != 0 {
		add("(he.target_id = ? OR he.runtime_id IN (SELECT id FROM connector_runtime_surfaces WHERE target_id = ? AND status = 'active'))", filter.TargetID, filter.TargetID)
	}
	if filter.ProfileID != 0 {
		add("(he.profile_id = ? OR he.runtime_id IN (SELECT id FROM connector_runtime_surfaces WHERE profile_id = ? AND status = 'active'))", filter.ProfileID, filter.ProfileID)
	}
	if filter.LabelID != 0 {
		add("he.id IN (SELECT history_entry_id FROM history_entry_labels WHERE label_id = ?)", filter.LabelID)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		add("(he.title LIKE ? OR he.summary LIKE ? OR he.preview_json LIKE ? OR he.input_text LIKE ? OR he.input_json LIKE ? OR he.output_text LIKE ? OR he.output_json LIKE ? OR he.error LIKE ? OR he.target_name LIKE ? OR he.profile_label LIKE ? OR he.action_name LIKE ? OR COALESCE(tok.name, '') LIKE ? OR COALESCE(project.name, '') LIKE ?)", like, like, like, like, like, like, like, like, like, like, like, like, like)
	}
	if filter.BeforeTime != "" && filter.BeforeID > 0 {
		add("(he.created_at, he.id) < (?, ?)", filter.BeforeTime, filter.BeforeID)
	}
	return strings.Join(where, " AND "), args
}

func scanEntry(scanner interface{ Scan(...any) error }) (Entry, error) {
	var item Entry
	var tokenID, projectID, runtimeID, targetID, profileID, exitCode sql.NullInt64
	var startedAt, completedAt sql.NullString
	var approvalRequired int
	err := scanner.Scan(&item.ID, &item.SourceRefType, &item.SourceRefID, &item.ConnectorKind,
		&item.ActivityType, &tokenID, &item.TokenName, &projectID, &item.ProjectName, &runtimeID,
		&targetID, &profileID, &item.TargetName, &item.ProfileLabel, &item.Source, &item.Status,
		&item.ActionName, &item.Title, &item.Summary, &item.PreviewJSON, &item.InputText, &item.InputJSON,
		&item.OutputText, &item.OutputJSON, &item.Error, &item.RetryPolicyJSON, &exitCode,
		&item.ProgressCurrent, &item.ProgressTotal, &item.BytesDone, &item.BytesTotal, &approvalRequired,
		&item.UserNote, &item.CreatedAt, &startedAt, &completedAt, &item.UpdatedAt)
	if err != nil {
		return Entry{}, err
	}
	item.TokenID, item.ProjectID, item.RuntimeID = nullableInt64(tokenID), nullableInt64(projectID), nullableInt64(runtimeID)
	item.TargetID, item.ProfileID = nullableInt64(targetID), nullableInt64(profileID)
	if exitCode.Valid {
		value := int(exitCode.Int64)
		item.ExitCode = &value
	}
	if startedAt.Valid {
		value := startedAt.String
		item.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.String
		item.CompletedAt = &value
	}
	if item.SourceRefType == "connector_action_request" {
		var policy connectors.RetryPolicy
		if err := json.Unmarshal([]byte(item.RetryPolicyJSON), &policy); err != nil {
			return Entry{}, err
		}
		encoded, err := json.Marshal(connectors.NormalizePersistedRetryPolicy(policy))
		if err != nil {
			return Entry{}, err
		}
		item.RetryPolicyJSON = string(encoded)
	} else {
		item.RetryPolicyJSON = ""
	}
	item.ApprovalRequired = approvalRequired != 0
	item.Labels = []Label{}
	return item, nil
}

func (s *QueryStore) attachLabels(ctx context.Context, items []Entry) error {
	if len(items) == 0 {
		return nil
	}
	ids, args, byID := make([]string, 0, len(items)), make([]any, 0, len(items)), map[int64]int{}
	for index := range items {
		items[index].Labels = []Label{}
		ids, args = append(ids, "?"), append(args, items[index].ID)
		byID[items[index].ID] = index
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT hel.history_entry_id, hl.id, hl.name, hl.color, hl.created_at, hl.updated_at
		FROM history_entry_labels hel JOIN history_labels hl ON hl.id = hel.label_id
		WHERE hel.history_entry_id IN (`+strings.Join(ids, ",")+`) ORDER BY lower(hl.name), hl.id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var entryID int64
		var label Label
		if err := rows.Scan(&entryID, &label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt); err != nil {
			return err
		}
		if index, ok := byID[entryID]; ok {
			items[index].Labels = append(items[index].Labels, label)
		}
	}
	return rows.Err()
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
