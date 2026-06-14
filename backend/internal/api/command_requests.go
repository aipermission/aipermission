package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/history"
)

type commandRequestInsert struct {
	TokenID          *int64
	RuntimeProfileID int64
	Source           string
	Command          string
	Reason           string
	Status           string
}

func (s *Server) insertCommandRequest(ctx context.Context, runtime *databaseRuntime, tokenID int64, runtimeProfileID int64, command string, reason string, status string) (int64, error) {
	return s.insertCommandRequestWithOptions(ctx, runtime, commandRequestInsert{
		TokenID:          &tokenID,
		RuntimeProfileID: runtimeProfileID,
		Source:           commandRequestSourceMCP,
		Command:          command,
		Reason:           reason,
		Status:           status,
	})
}

func (s *Server) insertCommandRequestWithOptions(ctx context.Context, runtime *databaseRuntime, request commandRequestInsert) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	encryptedCommand, err := runtime.vault.EncryptJSON(request.Command)
	if err != nil {
		return 0, fmt.Errorf("encrypt command payload: %w", err)
	}
	if request.Source == "" {
		request.Source = commandRequestSourceMCP
	}
	storedCommand := s.redactForPersistence(ctx, runtime, request.Command)
	storedReason := s.redactForPersistence(ctx, runtime, request.Reason)
	result, err := runtime.database.ExecContext(ctx, `
		INSERT INTO command_requests (token_id, runtime_profile_id, source, command, encrypted_command, reason, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableInt64(request.TokenID),
		request.RuntimeProfileID,
		request.Source,
		storedCommand,
		encryptedCommand,
		storedReason,
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
	if err := history.NewStore(runtime.database).SyncCommandRequest(ctx, id); err != nil {
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
	if err := runtime.vault.DecryptJSON(encryptedCommand, &command); err != nil {
		return "", fmt.Errorf("decrypt command payload: %w", err)
	}
	return command, nil
}

func (s *Server) finishActiveCommandRequest(runtime *databaseRuntime, requestID int64, runtimeProfileID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpBackgroundCommandTimeout)
	defer cancel()
	result, err := runtime.consoleSessions.WaitActive(ctx, runtimeProfileID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			_ = runtime.consoleSessions.InterruptActive(context.Background(), runtimeProfileID)
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
	if _, err := runtime.database.ExecContext(ctx, `UPDATE command_requests SET session_id = ? WHERE id = ?`, sessionID, id); err != nil {
		return err
	}
	return history.NewStore(runtime.database).SyncCommandRequest(ctx, id)
}

func (s *Server) finishCommandRequest(ctx context.Context, runtime *databaseRuntime, id int64, status string, sessionID int64, stdout string, stderr string, exitCode int, errorText string) error {
	stdout = console.PlainOutput(stdout)
	stderr = console.PlainOutput(stderr)
	stdout = s.redactForPersistence(ctx, runtime, stdout)
	stderr = s.redactForPersistence(ctx, runtime, stderr)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = ?, session_id = NULLIF(?, 0), stdout = ?, stderr = ?, exit_code = ?, error = ?, completed_at = ?
		WHERE id = ? AND status = 'running'`,
		status,
		sessionID,
		stdout,
		stderr,
		exitCode,
		errorText,
		now,
		id,
	); err != nil {
		return err
	}
	return history.NewStore(runtime.database).SyncCommandRequest(ctx, id)
}

func (s *Server) commandRequestIDs(ctx context.Context, runtime *databaseRuntime, where string, args ...any) ([]int64, error) {
	if runtime == nil || runtime.database == nil {
		return nil, nil
	}
	rows, err := runtime.database.QueryContext(ctx, `SELECT id FROM command_requests WHERE `+where, args...)
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

func (s *Server) syncCommandRequestIDs(ctx context.Context, runtime *databaseRuntime, ids []int64) error {
	if runtime == nil || runtime.database == nil {
		return nil
	}
	store := history.NewStore(runtime.database)
	for _, id := range ids {
		if err := store.SyncCommandRequest(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) cancelRunningCommandRequests(ctx context.Context, runtime *databaseRuntime, errorText string) error {
	if runtime == nil || runtime.database == nil {
		return nil
	}
	ids, err := s.commandRequestIDs(ctx, runtime, `status = 'running'`)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	_, err = runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
		WHERE status = 'running'`,
		errorText,
		now,
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "database is closed") {
		return nil
	}
	if err != nil {
		return err
	}
	return s.syncCommandRequestIDs(ctx, runtime, ids)
}

func (s *Server) cancelRunningCommandRequestsForSession(ctx context.Context, runtime *databaseRuntime, sessionID int64, errorText string) error {
	if runtime == nil || runtime.database == nil || sessionID < 1 {
		return nil
	}
	ids, err := s.commandRequestIDs(ctx, runtime, `status = 'running' AND session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	_, err = runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
		WHERE status = 'running' AND session_id = ?`,
		errorText,
		now,
		sessionID,
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "database is closed") {
		return nil
	}
	if err != nil {
		return err
	}
	return s.syncCommandRequestIDs(ctx, runtime, ids)
}

func (s *Server) cancelRunningCommandRequestsForServer(ctx context.Context, runtime *databaseRuntime, runtimeProfileID int64, errorText string) (int64, error) {
	if runtime == nil || runtime.database == nil || runtimeProfileID < 1 {
		return 0, nil
	}
	ids, err := s.commandRequestIDs(ctx, runtime, `status = 'running' AND runtime_profile_id = ?`, runtimeProfileID)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	result, err := runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
		WHERE status = 'running' AND runtime_profile_id = ?`,
		errorText,
		now,
		runtimeProfileID,
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "database is closed") {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if err := s.syncCommandRequestIDs(ctx, runtime, ids); err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
}
