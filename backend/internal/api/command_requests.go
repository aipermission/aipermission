package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type commandRequestInsert struct {
	TokenID   *int64
	RuntimeID int64
	Source    string
	Command   string
	Reason    string
	Status    string
}

type preparedCommandRequestInsert struct {
	commandRequestInsert
	storedCommand string
	storedReason  string
}

func (s *Server) insertCommandRequest(ctx context.Context, runtime *databaseRuntime, tokenID int64, runtimeID int64, command string, reason string, status string) (int64, error) {
	return s.insertCommandRequestWithOptions(ctx, runtime, commandRequestInsert{
		TokenID:   &tokenID,
		RuntimeID: runtimeID,
		Source:    commandRequestSourceMCP,
		Command:   command,
		Reason:    reason,
		Status:    status,
	})
}

func (s *Server) insertCommandRequestWithOptions(ctx context.Context, runtime *databaseRuntime, request commandRequestInsert) (int64, error) {
	prepared := s.prepareCommandRequestInsert(ctx, runtime, request)
	tx, err := runtime.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin command request insert: %w", err)
	}
	defer tx.Rollback()
	id, err := s.insertCommandRequestWithExecutor(ctx, runtime, tx, prepared)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit command request insert: %w", err)
	}
	return id, nil
}

func (s *Server) prepareCommandRequestInsert(ctx context.Context, runtime *databaseRuntime, request commandRequestInsert) preparedCommandRequestInsert {
	return preparedCommandRequestInsert{
		commandRequestInsert: request,
		storedCommand:        s.redactForPersistence(ctx, runtime, request.Command),
		storedReason:         s.redactForPersistence(ctx, runtime, request.Reason),
	}
}

func (s *Server) insertCommandRequestWithExecutor(ctx context.Context, runtime *databaseRuntime, executor history.CommandProjectionExecutor, request preparedCommandRequestInsert) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if request.Source == "" {
		request.Source = commandRequestSourceMCP
	}
	result, err := executor.ExecContext(ctx, `
		INSERT INTO command_requests (token_id, runtime_id, source, command, encrypted_command, reason, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableInt64(request.TokenID),
		request.RuntimeID,
		request.Source,
		request.storedCommand,
		"",
		request.storedReason,
		request.Status,
		now,
	)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	encryptedCommand, err := recordcrypto.EncryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.CommandRequest, id, request.Command)
	if err != nil {
		return 0, fmt.Errorf("encrypt command payload: %w", err)
	}
	if _, err := executor.ExecContext(ctx, `UPDATE command_requests SET encrypted_command = ? WHERE id = ?`, encryptedCommand, id); err != nil {
		return 0, fmt.Errorf("store encrypted command payload: %w", err)
	}
	if err := history.SyncCommandRequestWithExecutor(ctx, executor, id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Server) commandRequestExecutionCommand(ctx context.Context, runtime *databaseRuntime, id int64) (string, error) {
	var encryptedCommand string
	var displayCommand string
	err := runtime.database.QueryRowContext(ctx, `
		SELECT encrypted_command, command
		FROM command_requests
		WHERE id = ?`,
		id,
	).Scan(&encryptedCommand, &displayCommand)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err != nil {
		return "", err
	}
	if encryptedCommand == "" {
		return displayCommand, nil
	}
	var command string
	if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.CommandRequest, id, encryptedCommand, &command); err != nil {
		return "", fmt.Errorf("decrypt command payload: %w", err)
	}
	return command, nil
}

func (s *Server) finishActiveCommandRequest(runtime *databaseRuntime, requestID int64, principal executionprincipal.Principal, handle console.SessionHandle) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpBackgroundCommandTimeout)
	defer cancel()
	result, err := runtime.consoleSessions.WaitActive(ctx, principal, handle)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			_ = runtime.consoleSessions.InterruptActive(context.Background(), principal, handle)
			_ = s.finishCommandRequest(context.Background(), runtime, requestID, "error", 0, "", "", 0, "command timed out while running in background")
			return
		}
		_ = s.finishCommandRequest(context.Background(), runtime, requestID, "error", 0, "", "", 0, err.Error())
		return
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	_ = s.finishCommandRequest(context.Background(), runtime, requestID, status, result.SessionID, result.Output, "", result.ExitCode, "")
}

func (s *Server) setCommandRequestSession(ctx context.Context, runtime *databaseRuntime, id int64, sessionID int64) error {
	return withCommandProjectionTransaction(ctx, runtime, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE command_requests SET session_id = ? WHERE id = ?`, sessionID, id); err != nil {
			return err
		}
		return history.SyncCommandRequestWithExecutor(ctx, tx, id)
	})
}

func (s *Server) finishCommandRequest(ctx context.Context, runtime *databaseRuntime, id int64, status string, sessionID int64, stdout string, stderr string, exitCode int, errorText string) error {
	stdout = console.PlainOutput(stdout)
	stderr = console.PlainOutput(stderr)
	stdout = s.redactForPersistence(ctx, runtime, stdout)
	stderr = s.redactForPersistence(ctx, runtime, stderr)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	now := time.Now().UTC().Format(time.RFC3339)
	return withCommandProjectionTransaction(ctx, runtime, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE command_requests
			SET status = ?, session_id = NULLIF(?, 0), stdout = ?, stderr = ?, exit_code = ?, error = ?, completed_at = ?
			WHERE id = ? AND status = 'running'`,
			status, sessionID, stdout, stderr, exitCode, errorText, now, id,
		); err != nil {
			return err
		}
		return history.SyncCommandRequestWithExecutor(ctx, tx, id)
	})
}

func withCommandProjectionTransaction(ctx context.Context, runtime *databaseRuntime, mutate func(*sql.Tx) error) error {
	if runtime == nil || runtime.database == nil {
		return fmt.Errorf("command request database is unavailable")
	}
	tx, err := runtime.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mutate(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func commandRequestIDs(ctx context.Context, executor history.CommandProjectionExecutor, where string, args ...any) ([]int64, error) {
	if executor == nil {
		return nil, nil
	}
	rows, err := executor.QueryContext(ctx, `SELECT id FROM command_requests WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func syncCommandRequestIDs(ctx context.Context, executor history.CommandProjectionExecutor, ids []int64) error {
	if executor == nil {
		return nil
	}
	for _, id := range ids {
		if err := history.SyncCommandRequestWithExecutor(ctx, executor, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) cancelRunningCommandRequests(ctx context.Context, runtime *databaseRuntime, errorText string) error {
	if runtime == nil || runtime.database == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	err := withCommandProjectionTransaction(ctx, runtime, func(tx *sql.Tx) error {
		ids, err := commandRequestIDs(ctx, tx, `status = 'running'`)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE command_requests
			SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
			WHERE status = 'running'`, errorText, now); err != nil {
			return err
		}
		return syncCommandRequestIDs(ctx, tx, ids)
	})
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "database is closed") {
		return nil
	}
	return err
}

func (s *Server) cancelRunningCommandRequestsForSession(ctx context.Context, runtime *databaseRuntime, sessionID int64, errorText string) error {
	if runtime == nil || runtime.database == nil || sessionID < 1 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	err := withCommandProjectionTransaction(ctx, runtime, func(tx *sql.Tx) error {
		ids, err := commandRequestIDs(ctx, tx, `status = 'running' AND session_id = ?`, sessionID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE command_requests
			SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
			WHERE status = 'running' AND session_id = ?`, errorText, now, sessionID); err != nil {
			return err
		}
		return syncCommandRequestIDs(ctx, tx, ids)
	})
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "database is closed") {
		return nil
	}
	return err
}

func (s *Server) cancelRunningCommandRequestsForServer(ctx context.Context, runtime *databaseRuntime, runtimeID int64, errorText string) (int64, error) {
	if runtime == nil || runtime.database == nil || runtimeID < 1 {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	var affected int64
	err := withCommandProjectionTransaction(ctx, runtime, func(tx *sql.Tx) error {
		ids, err := commandRequestIDs(ctx, tx, `status = 'running' AND runtime_id = ?`, runtimeID)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE command_requests
			SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
			WHERE status = 'running' AND runtime_id = ?`, errorText, now, runtimeID)
		if err != nil {
			return err
		}
		if err := syncCommandRequestIDs(ctx, tx, ids); err != nil {
			return err
		}
		affected, _ = result.RowsAffected()
		return nil
	})
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "database is closed") {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return affected, nil
}
