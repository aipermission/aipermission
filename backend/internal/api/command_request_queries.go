package api

import (
	"context"
	"database/sql"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/console"
)

const (
	runningCommandRequestAssistantHint = "Wait 3 seconds, then poll this running console command request again."

	commandRequestSourceMCP    = "mcp"
	commandRequestSourceManual = "manual"
)

type commandRequestFilter struct {
	TokenID   int64
	Source    string
	Status    string
	RuntimeID int64
	Query     string
	Limit     int
	Offset    int
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Server) listCommandRequests(ctx context.Context, runtime *databaseRuntime, filter commandRequestFilter) ([]commandRequestRecord, error) {
	where := []string{"(? = '' OR cr.source = ?)", "(? = 0 OR cr.token_id = ?)", "(? = 0 OR cr.runtime_id = ?)", "(? = '' OR cr.status = ?)"}
	args := []any{filter.Source, filter.Source, filter.TokenID, filter.TokenID, filter.RuntimeID, filter.RuntimeID, filter.Status, filter.Status}
	query := `
		SELECT cr.id, cr.token_id, COALESCE(tok.name, ''), cr.runtime_id, COALESCE(ct.name, ''), cr.source, cr.command, cr.reason, cr.status,
		       cr.tracking_reason, cr.output_truncated, cr.stdout, cr.stderr, cr.exit_code, cr.session_id, cr.user_note, cr.error, cr.created_at, cr.completed_at
		FROM command_requests cr
			LEFT JOIN connector_runtime_surfaces rs ON rs.id = cr.runtime_id
			LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
			LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY cr.created_at DESC, cr.id DESC
		LIMIT 100`
	rows, err := runtime.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []commandRequestRecord{}
	for rows.Next() {
		item, err := scanCommandRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) listCommandRequestSummaries(ctx context.Context, runtime *databaseRuntime, filter commandRequestFilter) ([]commandRequestRecord, int, error) {
	where := []string{"(? = '' OR cr.source = ?)", "(? = 0 OR cr.token_id = ?)", "(? = 0 OR cr.runtime_id = ?)", "(? = '' OR cr.status = ?)"}
	args := []any{filter.Source, filter.Source, filter.TokenID, filter.TokenID, filter.RuntimeID, filter.RuntimeID, filter.Status, filter.Status}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		if ftsQuery := buildFTSQuery(filter.Query); ftsQuery != "" {
			where = append(where, `(cr.id IN (SELECT rowid FROM command_requests_fts WHERE command_requests_fts MATCH ?) OR COALESCE(ct.name, '') LIKE ? OR COALESCE(tok.name, '') LIKE ?)`)
			args = append(args, ftsQuery, like, like)
		} else {
			where = append(where, `(cr.command LIKE ? OR cr.reason LIKE ? OR cr.status LIKE ? OR cr.source LIKE ? OR cr.tracking_reason LIKE ? OR cr.stdout LIKE ? OR cr.stderr LIKE ? OR cr.error LIKE ? OR COALESCE(ct.name, '') LIKE ? OR COALESCE(tok.name, '') LIKE ?)`)
			args = append(args, like, like, like, like, like, like, like, like, like, like)
		}
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := runtime.database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM command_requests cr
			LEFT JOIN connector_runtime_surfaces rs ON rs.id = cr.runtime_id
			LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
			LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE `+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT cr.id, cr.token_id, COALESCE(tok.name, ''), cr.runtime_id, COALESCE(ct.name, ''), cr.source, cr.command, cr.reason, cr.status,
		       cr.tracking_reason, cr.output_truncated, '' AS stdout, '' AS stderr, cr.exit_code, cr.session_id, cr.user_note, cr.error, cr.created_at, cr.completed_at
		FROM command_requests cr
			LEFT JOIN connector_runtime_surfaces rs ON rs.id = cr.runtime_id
			LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
			LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE `+whereSQL+`
		ORDER BY cr.created_at DESC, cr.id DESC
		LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []commandRequestRecord{}
	for rows.Next() {
		item, err := scanCommandRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Server) getCommandRequest(ctx context.Context, runtime *databaseRuntime, id int64, tokenID int64, source string) (commandRequestRecord, error) {
	row := runtime.database.QueryRowContext(ctx, `
		SELECT cr.id, cr.token_id, COALESCE(tok.name, ''), cr.runtime_id, COALESCE(ct.name, ''), cr.source, cr.command, cr.reason, cr.status,
		       cr.tracking_reason, cr.output_truncated, cr.stdout, cr.stderr, cr.exit_code, cr.session_id, cr.user_note, cr.error, cr.created_at, cr.completed_at
		FROM command_requests cr
			LEFT JOIN connector_runtime_surfaces rs ON rs.id = cr.runtime_id
			LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
			LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE cr.id = ? AND (? = '' OR cr.source = ?) AND (? = 0 OR cr.token_id = ?)`,
		id,
		source,
		source,
		tokenID,
		tokenID,
	)
	item, err := scanCommandRequest(row)
	if err != nil {
		return commandRequestRecord{}, err
	}
	return item, nil
}

func scanCommandRequest(scanner interface {
	Scan(dest ...any) error
}) (commandRequestRecord, error) {
	var item commandRequestRecord
	var tokenID sql.NullInt64
	var exitCode sql.NullInt64
	var sessionID sql.NullInt64
	var userNote sql.NullString
	var completedAt sql.NullString
	var outputTruncated int
	err := scanner.Scan(
		&item.ID,
		&tokenID,
		&item.TokenName,
		&item.RuntimeID,
		&item.TargetName,
		&item.Source,
		&item.Command,
		&item.Reason,
		&item.Status,
		&item.TrackingReason,
		&outputTruncated,
		&item.Stdout,
		&item.Stderr,
		&exitCode,
		&sessionID,
		&userNote,
		&item.Error,
		&item.CreatedAt,
		&completedAt,
	)
	if err != nil {
		return commandRequestRecord{}, err
	}
	if item.Source == "" {
		item.Source = commandRequestSourceMCP
	}
	item.OutputTruncated = outputTruncated != 0
	item.Stdout = console.PlainOutput(item.Stdout)
	item.Stderr = console.PlainOutput(item.Stderr)
	if tokenID.Valid {
		value := tokenID.Int64
		item.TokenID = &value
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		item.ExitCode = &value
	}
	if sessionID.Valid {
		value := sessionID.Int64
		item.SessionID = &value
	}
	if userNote.Valid {
		item.UserNote = stringPtr(userNote.String)
	}
	if completedAt.Valid {
		item.CompletedAt = stringPtr(completedAt.String)
	}
	item.PolicyWarnings = analyzeCommandPolicy(item.Command)
	annotateCommandRequestForAssistant(&item)
	return item, nil
}

func annotateCommandRequestForAssistant(item *commandRequestRecord) {
	item.RetryAfterSeconds = 0
	item.AssistantHint = ""
	if item.Status == "running" {
		item.RetryAfterSeconds = 3
		item.AssistantHint = runningCommandRequestAssistantHint
	}
}

func stringPtr(value string) *string {
	return &value
}
