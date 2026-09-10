package commandrequests

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const RunningAssistantHint = "Wait 3 seconds, then poll this running console command request again."

type Store struct {
	database sqldb.Executor
}

func NewStore(database sqldb.Executor) *Store {
	return &Store{database: database}
}

func (s *Store) Get(ctx context.Context, id, tokenID int64, source string) (Record, error) {
	if s == nil || s.database == nil {
		return Record{}, sql.ErrConnDone
	}
	row := s.database.QueryRowContext(ctx, `
		SELECT cr.id, cr.token_id, COALESCE(tok.name, ''), cr.runtime_id, COALESCE(ct.name, ''), cr.source, cr.command, cr.reason, cr.status,
		       cr.tracking_reason, cr.output_truncated, cr.stdout, cr.stderr, cr.exit_code, cr.session_id, cr.user_note, cr.error, cr.created_at, cr.completed_at
		FROM command_requests cr
			LEFT JOIN connector_runtime_surfaces rs ON rs.id = cr.runtime_id
			LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
			LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE cr.id = ? AND (? = '' OR cr.source = ?) AND (? = 0 OR cr.token_id = ?)`,
		id, source, source, tokenID, tokenID,
	)
	return scanRecord(row)
}

func scanRecord(scanner interface{ Scan(...any) error }) (Record, error) {
	var item Record
	var tokenID, exitCode, sessionID sql.NullInt64
	var userNote, completedAt sql.NullString
	var outputTruncated int
	if err := scanner.Scan(
		&item.ID, &tokenID, &item.TokenName, &item.RuntimeID, &item.TargetName,
		&item.Source, &item.Command, &item.Reason, &item.Status, &item.TrackingReason,
		&outputTruncated, &item.Stdout, &item.Stderr, &exitCode, &sessionID,
		&userNote, &item.Error, &item.CreatedAt, &completedAt,
	); err != nil {
		return Record{}, err
	}
	if item.Source == "" {
		item.Source = SourceMCP
	}
	item.OutputTruncated = outputTruncated != 0
	item.Stdout = console.PlainOutput(item.Stdout)
	item.Stderr = console.PlainOutput(item.Stderr)
	if tokenID.Valid {
		item.TokenID = int64Pointer(tokenID.Int64)
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		item.ExitCode = &value
	}
	if sessionID.Valid {
		item.SessionID = int64Pointer(sessionID.Int64)
	}
	if userNote.Valid {
		item.UserNote = stringPointer(userNote.String)
	}
	if completedAt.Valid {
		item.CompletedAt = stringPointer(completedAt.String)
	}
	item.PolicyWarnings = AnalyzePolicy(item.Command)
	if item.Status == "running" {
		item.RetryAfterSeconds = 3
		item.AssistantHint = RunningAssistantHint
	}
	return item, nil
}

func int64Pointer(value int64) *int64    { return &value }
func stringPointer(value string) *string { return &value }
