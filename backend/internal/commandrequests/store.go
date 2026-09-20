package commandrequests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
	"github.com/aipermission/aipermission/backend/internal/timeformat"
)

var (
	ErrStoreUnavailable = errors.New("command request store is unavailable")
	ErrNotRunning       = errors.New("command request is no longer running")
)

type CommandCodec interface {
	Seal(int64, string) (string, error)
	Open(int64, string) (string, error)
}

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Database guarantees that every mutation can keep its history projection in
// the same transaction. Read-only executors are deliberately insufficient.
type Database interface {
	Executor
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type Projection interface {
	SyncCommandRequest(context.Context, Executor, int64) error
}

func (s *Store) Insert(
	ctx context.Context,
	codec CommandCodec,
	projection Projection,
	request PreparedInsert,
) (int64, error) {
	if s == nil || s.database == nil || codec == nil || projection == nil {
		return 0, ErrStoreUnavailable
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin command request insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	id, err := s.InsertWithExecutor(ctx, tx, codec, projection, request)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit command request insert: %w", err)
	}
	return id, nil
}

func (s *Store) InsertWithExecutor(
	ctx context.Context,
	executor Executor,
	codec CommandCodec,
	projection Projection,
	request PreparedInsert,
) (int64, error) {
	if s == nil || executor == nil || codec == nil || projection == nil {
		return 0, ErrStoreUnavailable
	}
	if request.insert.Source == "" {
		request.insert.Source = SourceMCP
	}
	result, err := executor.ExecContext(ctx, `
		INSERT INTO command_requests (token_id, runtime_id, source, command, encrypted_command, reason, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableTokenID(request.insert.TokenID), request.insert.RuntimeID, request.insert.Source,
		request.storedCommand, "", request.storedReason, request.insert.Status,
		timeformat.UTC(time.Now()),
	)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	sealed, err := codec.Seal(id, request.insert.Command)
	if err != nil {
		return 0, fmt.Errorf("encrypt command payload: %w", err)
	}
	if _, err := executor.ExecContext(ctx, `UPDATE command_requests SET encrypted_command = ? WHERE id = ?`, sealed, id); err != nil {
		return 0, fmt.Errorf("store encrypted command payload: %w", err)
	}
	if err := projection.SyncCommandRequest(ctx, executor, id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) ExecutionCommand(ctx context.Context, codec CommandCodec, id int64) (string, error) {
	if s == nil || s.database == nil || codec == nil {
		return "", ErrStoreUnavailable
	}
	var sealed, display string
	if err := s.database.QueryRowContext(ctx, `
		SELECT encrypted_command, command FROM command_requests WHERE id = ?`, id,
	).Scan(&sealed, &display); err != nil {
		return "", err
	}
	if sealed == "" {
		return display, nil
	}
	command, err := codec.Open(id, sealed)
	if err != nil {
		return "", fmt.Errorf("decrypt command payload: %w", err)
	}
	return command, nil
}

func (s *Store) SetSession(ctx context.Context, projection Projection, id, sessionID int64) error {
	return s.withProjectionTransaction(ctx, projection, func(executor Executor) ([]int64, error) {
		if _, err := executor.ExecContext(ctx, `UPDATE command_requests SET session_id = ? WHERE id = ?`, sessionID, id); err != nil {
			return nil, err
		}
		return []int64{id}, nil
	})
}

type Completion struct {
	ID        int64
	Status    string
	SessionID int64
	Stdout    string
	Stderr    string
	ExitCode  int
	Error     string
}

func (s *Store) Finish(ctx context.Context, projection Projection, completion Completion) error {
	return s.withProjectionTransaction(ctx, projection, func(executor Executor) ([]int64, error) {
		result, err := executor.ExecContext(ctx, `
			UPDATE command_requests
			SET status = ?, session_id = NULLIF(?, 0), stdout = ?, stderr = ?, exit_code = ?, error = ?, completed_at = ?
			WHERE id = ? AND status = 'running'`,
			completion.Status, completion.SessionID, completion.Stdout, completion.Stderr,
			completion.ExitCode, completion.Error, timeformat.UTC(time.Now()), completion.ID,
		)
		if err != nil {
			return nil, err
		}
		affected, err := sqldb.RowsAffected(result, "finish command request")
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			return nil, ErrNotRunning
		}
		return []int64{completion.ID}, nil
	})
}

func (s *Store) Status(ctx context.Context, id int64) (string, error) {
	if s == nil || s.database == nil {
		return "", ErrStoreUnavailable
	}
	var status string
	err := s.database.QueryRowContext(ctx, `SELECT status FROM command_requests WHERE id = ?`, id).Scan(&status)
	return status, err
}

func (s *Store) CancelRunning(ctx context.Context, projection Projection, errorText string) error {
	return s.cancel(ctx, projection, "status = 'running'", nil, errorText)
}

func (s *Store) MarkRunningOutcomeUnknown(ctx context.Context, projection Projection, errorText string) error {
	return s.finishRunning(ctx, projection, "outcome_unknown", errorText)
}

func (s *Store) CancelRunningForSession(
	ctx context.Context,
	projection Projection,
	sessionID int64,
	errorText string,
) error {
	if sessionID < 1 {
		return nil
	}
	return s.cancel(ctx, projection, "status = 'running' AND session_id = ?", []any{sessionID}, errorText)
}

func (s *Store) CancelRunningForRuntime(
	ctx context.Context,
	projection Projection,
	runtimeID int64,
	errorText string,
) (int64, error) {
	if runtimeID < 1 {
		return 0, nil
	}
	var affected int64
	err := s.withProjectionTransaction(ctx, projection, func(executor Executor) ([]int64, error) {
		ids, err := requestIDs(ctx, executor, "status = 'running' AND runtime_id = ?", runtimeID)
		if err != nil {
			return nil, err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE command_requests
			SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
			WHERE status = 'running' AND runtime_id = ?`,
			errorText, timeformat.UTC(time.Now()), runtimeID,
		)
		if err != nil {
			return nil, err
		}
		affected, err = sqldb.RowsAffected(result, "cancel running command requests for runtime")
		return ids, err
	})
	return affected, err
}

func (s *Store) cancel(
	ctx context.Context,
	projection Projection,
	where string,
	args []any,
	errorText string,
) error {
	return s.withProjectionTransaction(ctx, projection, func(executor Executor) ([]int64, error) {
		ids, err := requestIDs(ctx, executor, where, args...)
		if err != nil {
			return nil, err
		}
		query := `UPDATE command_requests
			SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
			WHERE ` + where
		_, err = executor.ExecContext(ctx, query, append([]any{errorText, timeformat.UTC(time.Now())}, args...)...)
		return ids, err
	})
}

func (s *Store) finishRunning(ctx context.Context, projection Projection, status, errorText string) error {
	return s.withProjectionTransaction(ctx, projection, func(executor Executor) ([]int64, error) {
		ids, err := requestIDs(ctx, executor, "status = 'running'")
		if err != nil {
			return nil, err
		}
		_, err = executor.ExecContext(ctx, `
			UPDATE command_requests
			SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?)
			WHERE status = 'running'`,
			status, errorText, timeformat.UTC(time.Now()),
		)
		return ids, err
	})
}

func (s *Store) withProjectionTransaction(
	ctx context.Context,
	projection Projection,
	mutate func(Executor) ([]int64, error),
) error {
	if s == nil || s.database == nil || projection == nil || mutate == nil {
		return ErrStoreUnavailable
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin command request mutation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	ids, err := mutate(tx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := projection.SyncCommandRequest(ctx, tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func requestIDs(ctx context.Context, executor Executor, where string, args ...any) ([]int64, error) {
	rows, err := executor.QueryContext(ctx, `SELECT id FROM command_requests WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func nullableTokenID(tokenID *int64) any {
	if tokenID == nil || *tokenID == 0 {
		return nil
	}
	return *tokenID
}
