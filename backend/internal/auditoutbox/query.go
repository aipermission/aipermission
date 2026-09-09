package auditoutbox

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type QueryFilter struct {
	Actor         string
	RuntimeID     int64
	ProjectID     int64
	ConnectorKind string
	TargetID      int64
	Query         string
	Limit         int
	Offset        int
}

type Record struct {
	ID              int64  `json:"id"`
	EventVersion    int    `json:"event_version"`
	ActorType       string `json:"actor_type"`
	TokenID         *int64 `json:"token_id,omitempty"`
	TokenName       string `json:"token_name,omitempty"`
	ProjectID       *int64 `json:"project_id,omitempty"`
	ProjectName     string `json:"project_name,omitempty"`
	RuntimeID       *int64 `json:"runtime_id,omitempty"`
	ConnectorKind   string `json:"connector_kind,omitempty"`
	TargetID        *int64 `json:"target_id,omitempty"`
	TargetName      string `json:"target_name,omitempty"`
	ProfileID       *int64 `json:"profile_id,omitempty"`
	ActionRequestID *int64 `json:"action_request_id,omitempty"`
	Action          string `json:"action"`
	LifecyclePhase  string `json:"lifecycle_phase"`
	PayloadJSON     string `json:"payload_json"`
	CreatedAt       string `json:"created_at"`
}

type QueryResult struct {
	Items []Record
	Total int
}

type QueryStore struct {
	executor sqldb.Executor
}

func NewQueryStore(executor sqldb.Executor) *QueryStore {
	return &QueryStore{executor: executor}
}

func (s *QueryStore) List(ctx context.Context, filter QueryFilter) (QueryResult, error) {
	if s == nil || s.executor == nil {
		return QueryResult{}, fmt.Errorf("list audit logs: executor is unavailable")
	}
	if filter.Limit < 1 {
		return QueryResult{}, fmt.Errorf("list audit logs: limit must be positive")
	}
	if filter.Offset < 0 {
		return QueryResult{}, fmt.Errorf("list audit logs: offset must not be negative")
	}
	where, args := auditQueryWhere(filter)
	joins := ""
	if filter.Query != "" {
		joins = auditQueryJoins
	}
	var total int
	if err := s.executor.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM audit_logs a`+joins+`
		WHERE `+where,
		args...,
	).Scan(&total); err != nil {
		return QueryResult{}, fmt.Errorf("count audit logs: %w", err)
	}

	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := s.executor.QueryContext(ctx, `
		SELECT a.id, a.event_version, a.actor_type, a.token_id, COALESCE(t.name, ''), a.project_id, COALESCE(project.name, ''), a.runtime_id,
			COALESCE(ct.name, profile_ct.name, ''), a.connector_kind, a.target_id, a.profile_id, a.action_request_id,
			a.action, a.lifecycle_phase, substr(a.payload_json, 1, 500), a.created_at
		FROM audit_logs a`+auditQueryJoins+`
		WHERE `+where+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return QueryResult{}, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()

	items := []Record{}
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return QueryResult{}, fmt.Errorf("scan audit log: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("iterate audit logs: %w", err)
	}
	return QueryResult{Items: items, Total: total}, nil
}

func (s *QueryStore) Get(ctx context.Context, id int64) (Record, error) {
	if s == nil || s.executor == nil {
		return Record{}, fmt.Errorf("get audit log: executor is unavailable")
	}
	if id < 1 {
		return Record{}, fmt.Errorf("get audit log: id must be positive")
	}
	row := s.executor.QueryRowContext(ctx, `
		SELECT a.id, a.event_version, a.actor_type, a.token_id, COALESCE(t.name, ''), a.project_id, COALESCE(project.name, ''), a.runtime_id,
			COALESCE(ct.name, profile_ct.name, ''), a.connector_kind, a.target_id, a.profile_id, a.action_request_id,
			a.action, a.lifecycle_phase, a.payload_json, a.created_at
		FROM audit_logs a`+auditQueryJoins+`
		WHERE a.id = ?`,
		id,
	)
	item, err := scanAuditLog(row)
	if err != nil {
		return Record{}, fmt.Errorf("get audit log: %w", err)
	}
	return item, nil
}

const auditQueryJoins = `
	LEFT JOIN api_tokens t ON t.id = a.token_id
	LEFT JOIN projects project ON project.id = a.project_id
	LEFT JOIN connector_runtime_surfaces profile_rs ON profile_rs.id = a.runtime_id
	LEFT JOIN connector_credential_profiles profile_cp ON profile_cp.id = profile_rs.profile_id AND profile_cp.target_id = profile_rs.target_id AND profile_cp.connector_kind = profile_rs.connector_kind
	LEFT JOIN connector_targets profile_ct ON profile_ct.id = profile_cp.target_id
	LEFT JOIN connector_targets ct ON ct.id = a.target_id`

func auditQueryWhere(filter QueryFilter) (string, []any) {
	where := []string{"1 = 1"}
	args := []any{}
	if filter.Actor != "" {
		where = append(where, "a.actor_type = ?")
		args = append(args, filter.Actor)
	}
	if filter.RuntimeID != 0 {
		where = append(where, "a.runtime_id = ?")
		args = append(args, filter.RuntimeID)
	}
	if filter.ProjectID != 0 {
		where = append(where, "a.project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.ConnectorKind != "" {
		where = append(where, "a.connector_kind = ?")
		args = append(args, filter.ConnectorKind)
	}
	if filter.TargetID != 0 {
		where = append(where, "a.target_id = ?")
		args = append(args, filter.TargetID)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		if ftsQuery := history.FTS(filter.Query); ftsQuery != "" {
			where = append(where, `(a.id IN (SELECT rowid FROM audit_logs_fts WHERE audit_logs_fts MATCH ?) OR COALESCE(t.name, '') LIKE ? OR COALESCE(project.name, '') LIKE ? OR COALESCE(profile_ct.name, '') LIKE ? OR COALESCE(ct.name, '') LIKE ?)`)
			args = append(args, ftsQuery, like, like, like, like)
		} else {
			where = append(where, `(a.action LIKE ? OR a.actor_type LIKE ? OR a.payload_json LIKE ? OR a.connector_kind LIKE ? OR COALESCE(t.name, '') LIKE ? OR COALESCE(project.name, '') LIKE ? OR COALESCE(profile_ct.name, '') LIKE ? OR COALESCE(ct.name, '') LIKE ?)`)
			args = append(args, like, like, like, like, like, like, like, like)
		}
	}
	return strings.Join(where, " AND "), args
}

func scanAuditLog(scanner interface {
	Scan(dest ...any) error
}) (Record, error) {
	var item Record
	var tokenID sql.NullInt64
	var projectID sql.NullInt64
	var runtimeID sql.NullInt64
	var targetID sql.NullInt64
	var profileID sql.NullInt64
	var actionRequestID sql.NullInt64
	if err := scanner.Scan(
		&item.ID,
		&item.EventVersion,
		&item.ActorType,
		&tokenID,
		&item.TokenName,
		&projectID,
		&item.ProjectName,
		&runtimeID,
		&item.TargetName,
		&item.ConnectorKind,
		&targetID,
		&profileID,
		&actionRequestID,
		&item.Action,
		&item.LifecyclePhase,
		&item.PayloadJSON,
		&item.CreatedAt,
	); err != nil {
		return Record{}, err
	}
	item.TokenID = nullableInt64Pointer(tokenID)
	item.ProjectID = nullableInt64Pointer(projectID)
	item.RuntimeID = nullableInt64Pointer(runtimeID)
	item.TargetID = nullableInt64Pointer(targetID)
	item.ProfileID = nullableInt64Pointer(profileID)
	item.ActionRequestID = nullableInt64Pointer(actionRequestID)
	return item, nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}
