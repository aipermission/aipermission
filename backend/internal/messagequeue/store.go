// Package messagequeue owns persisted user-to-agent and agent-to-user notes.
package messagequeue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const MaxMessageBytes = 8 << 10

type Redactor func(context.Context, string) string

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type Store struct {
	database *sql.DB
	redact   Redactor
}

func NewStore(database *sql.DB, redact Redactor) *Store {
	return &Store{database: database, redact: redact}
}

type Record struct {
	ID         int64   `json:"id"`
	TokenID    int64   `json:"token_id"`
	TokenName  string  `json:"token_name,omitempty"`
	RuntimeID  *int64  `json:"runtime_id,omitempty"`
	TargetName string  `json:"target_name,omitempty"`
	SessionID  *int64  `json:"session_id,omitempty"`
	Direction  string  `json:"direction"`
	Message    string  `json:"message"`
	ConsumedAt *string `json:"consumed_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

type CreateRequest struct {
	TokenID   int64  `json:"token_id"`
	RuntimeID *int64 `json:"runtime_id"`
	SessionID *int64 `json:"session_id"`
	Direction string `json:"direction"`
	Message   string `json:"message"`
}

type Filter struct {
	TokenID   int64
	RuntimeID int64
	Direction string
}

func (s *Store) Insert(ctx context.Context, request CreateRequest) (Record, error) {
	if err := s.ready(); err != nil {
		return Record{}, err
	}
	request.Message = strings.TrimSpace(request.Message)
	request.Direction = strings.TrimSpace(request.Direction)
	if request.Direction == "" {
		request.Direction = "user_to_ai"
	}
	if request.Direction != "user_to_ai" && request.Direction != "ai_to_user" {
		return Record{}, errors.New("direction must be user_to_ai or ai_to_user")
	}
	if request.TokenID < 1 {
		return Record{}, errors.New("token_id is required")
	}
	if request.Message == "" {
		return Record{}, errors.New("message is required")
	}
	if len(request.Message) > MaxMessageBytes {
		return Record{}, fmt.Errorf("message must be %d bytes or less", MaxMessageBytes)
	}
	if err := s.validateScope(ctx, &request); err != nil {
		return Record{}, err
	}
	if s.redact != nil {
		request.Message = s.redact(ctx, request.Message)
	}

	result, err := s.database.ExecContext(ctx, `
		INSERT INTO message_queue (token_id, runtime_id, session_id, direction, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		request.TokenID, nullableID(request.RuntimeID), nullableID(request.SessionID),
		request.Direction, request.Message, nowUTC(),
	)
	if err != nil {
		return Record{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Record{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id int64) (Record, error) {
	if err := s.ready(); err != nil {
		return Record{}, err
	}
	return scanRecord(s.database.QueryRowContext(ctx, selectSQL()+` WHERE mq.id = ?`, id))
}

func (s *Store) List(ctx context.Context, filter Filter) ([]Record, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	where := []string{"1 = 1"}
	args := make([]any, 0, 3)
	if filter.TokenID > 0 {
		where = append(where, "mq.token_id = ?")
		args = append(args, filter.TokenID)
	}
	if filter.RuntimeID > 0 {
		where = append(where, "mq.runtime_id = ?")
		args = append(args, filter.RuntimeID)
	}
	if filter.Direction != "" {
		where = append(where, "mq.direction = ?")
		args = append(args, filter.Direction)
	}
	rows, err := s.database.QueryContext(ctx, selectSQL()+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY mq.created_at DESC, mq.id DESC
		LIMIT 100`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Record{}
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkRuntimeRead(ctx context.Context, runtimeID int64) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	result, err := s.database.ExecContext(ctx, `
		UPDATE message_queue
		SET consumed_at = ?
		WHERE direction = 'ai_to_user' AND consumed_at IS NULL AND runtime_id = ?`,
		nowUTC(), runtimeID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func EnqueueUserNote(ctx context.Context, executor Executor, tokenID int64, message string) error {
	if executor == nil {
		return errors.New("message queue executor is unavailable")
	}
	message = strings.TrimSpace(message)
	if tokenID < 1 {
		return errors.New("token_id is required")
	}
	if message == "" {
		return errors.New("message is required")
	}
	if len(message) > MaxMessageBytes {
		return fmt.Errorf("message must be %d bytes or less", MaxMessageBytes)
	}
	_, err := executor.ExecContext(ctx, `
		INSERT INTO message_queue (token_id, direction, message, created_at)
		VALUES (?, 'user_to_ai', ?, ?)`, tokenID, message, nowUTC())
	return err
}

func (s *Store) ConsumeNextUser(ctx context.Context, tokenID, runtimeID, sessionID int64) (*string, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int64
	var message string
	err = tx.QueryRowContext(ctx, `SELECT mq.id, mq.message FROM message_queue mq`+nextUserClause(), nextUserArgs(tokenID, runtimeID, sessionID)...).Scan(&id, &message)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE message_queue SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`, nowUTC(), id)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, errors.New("message was already consumed")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &message, nil
}

func (s *Store) NextUser(ctx context.Context, tokenID, runtimeID, sessionID int64) (Record, error) {
	if err := s.ready(); err != nil {
		return Record{}, err
	}
	return scanRecord(s.database.QueryRowContext(ctx, selectSQL()+nextUserClause(), nextUserArgs(tokenID, runtimeID, sessionID)...))
}

func (s *Store) validateScope(ctx context.Context, request *CreateRequest) error {
	var exists int
	if err := s.database.QueryRowContext(ctx, `SELECT 1 FROM api_tokens WHERE id = ?`, request.TokenID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return errors.New("token_id does not exist")
	} else if err != nil {
		return err
	}
	if request.SessionID != nil {
		var sessionRuntimeID int64
		if err := s.database.QueryRowContext(ctx, `SELECT runtime_id FROM console_sessions WHERE id = ?`, *request.SessionID).Scan(&sessionRuntimeID); errors.Is(err, sql.ErrNoRows) {
			return errors.New("session_id does not exist")
		} else if err != nil {
			return err
		}
		if request.RuntimeID != nil && *request.RuntimeID != sessionRuntimeID {
			return errors.New("session_id does not belong to runtime_id")
		}
		request.RuntimeID = &sessionRuntimeID
	}
	if request.RuntimeID != nil {
		if err := s.database.QueryRowContext(ctx, `
			SELECT 1
			FROM connector_runtime_surfaces rs
			JOIN connector_targets ct ON ct.id = rs.target_id AND ct.connector_kind = rs.connector_kind
			JOIN connector_credential_profiles cp
			  ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
			WHERE rs.id = ? AND rs.capability_kind = 'live_console'
			  AND rs.status = 'active' AND ct.status = 'active' AND cp.status = 'active'`, *request.RuntimeID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return errors.New("runtime_id does not exist")
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ready() error {
	if s == nil || s.database == nil {
		return errors.New("message queue database is unavailable")
	}
	return nil
}

func nextUserClause() string {
	return `
		WHERE mq.token_id = ? AND mq.direction = 'user_to_ai' AND mq.consumed_at IS NULL
			AND ((? > 0 AND mq.runtime_id = ?) OR mq.runtime_id IS NULL)
			AND ((? > 0 AND mq.session_id = ?) OR mq.session_id IS NULL)
		ORDER BY
			CASE
				WHEN ? > 0 AND mq.session_id = ? THEN 0
				WHEN mq.runtime_id = ? THEN 1
				ELSE 2
			END,
			mq.created_at ASC,
			mq.id ASC
		LIMIT 1`
}

func nextUserArgs(tokenID, runtimeID, sessionID int64) []any {
	return []any{tokenID, runtimeID, runtimeID, sessionID, sessionID, sessionID, sessionID, runtimeID}
}

func selectSQL() string {
	return `
		SELECT mq.id, mq.token_id, COALESCE(tok.name, ''), mq.runtime_id, COALESCE(ct.name, ''), mq.session_id,
		       mq.direction, mq.message, mq.consumed_at, mq.created_at
		FROM message_queue mq
		JOIN api_tokens tok ON tok.id = mq.token_id
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = mq.runtime_id
		LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind`
}

func scanRecord(scanner interface{ Scan(...any) error }) (Record, error) {
	var item Record
	var runtimeID, sessionID sql.NullInt64
	var consumedAt sql.NullString
	err := scanner.Scan(&item.ID, &item.TokenID, &item.TokenName, &runtimeID, &item.TargetName,
		&sessionID, &item.Direction, &item.Message, &consumedAt, &item.CreatedAt)
	if err != nil {
		return Record{}, err
	}
	if runtimeID.Valid {
		item.RuntimeID = &runtimeID.Int64
	}
	if sessionID.Valid {
		item.SessionID = &sessionID.Int64
	}
	if consumedAt.Valid {
		item.ConsumedAt = &consumedAt.String
	}
	return item, nil
}

func nullableID(value *int64) any {
	if value == nil || *value == 0 {
		return nil
	}
	return *value
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }
