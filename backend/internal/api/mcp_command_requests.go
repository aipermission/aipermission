package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
)

func (s *Server) insertCommandRequest(ctx context.Context, runtime *databaseRuntime, tokenID int64, serverID int64, command string, reason string, status string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	encryptedCommand, err := runtime.vault.EncryptJSON(command)
	if err != nil {
		return 0, fmt.Errorf("encrypt command payload: %w", err)
	}
	approvalContext := ""
	approvalContextHash := ""
	if status == "pending_approval" && tokenID > 0 {
		approvalContext, approvalContextHash, err = s.buildApprovalContextSnapshot(ctx, runtime, tokenID, serverID, command, now)
		if err != nil {
			return 0, fmt.Errorf("build approval context snapshot: %w", err)
		}
	}
	storedCommand := s.redactForPersistence(ctx, runtime, command)
	storedReason := s.redactForPersistence(ctx, runtime, reason)
	result, err := runtime.database.ExecContext(ctx, `
		INSERT INTO command_requests (token_id, server_id, source, command, encrypted_command, reason, status, approval_context, approval_context_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenID,
		serverID,
		commandRequestSourceMCP,
		storedCommand,
		encryptedCommand,
		storedReason,
		status,
		approvalContext,
		approvalContextHash,
		now,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Server) commandRequestApprovalContextHash(ctx context.Context, runtime *databaseRuntime, id int64) string {
	var value string
	if err := runtime.database.QueryRowContext(ctx, `SELECT approval_context_hash FROM command_requests WHERE id = ?`, id).Scan(&value); err != nil {
		return ""
	}
	return value
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

func (s *Server) finishActiveCommandRequest(runtime *databaseRuntime, requestID int64, serverID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpBackgroundCommandTimeout)
	defer cancel()
	result, err := runtime.consoleSessions.WaitActive(ctx, serverID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			_ = runtime.consoleSessions.InterruptActive(context.Background(), serverID)
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
	_, err := runtime.database.ExecContext(ctx, `UPDATE command_requests SET session_id = ? WHERE id = ?`, sessionID, id)
	return err
}

func (s *Server) finishCommandRequest(ctx context.Context, runtime *databaseRuntime, id int64, status string, sessionID int64, stdout string, stderr string, exitCode int, errorText string) error {
	stdout = console.PlainOutput(stdout)
	stderr = console.PlainOutput(stderr)
	stdout = s.redactForPersistence(ctx, runtime, stdout)
	stderr = s.redactForPersistence(ctx, runtime, stderr)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := runtime.database.ExecContext(ctx, `
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
	)
	return err
}

func (s *Server) cancelRunningCommandRequests(ctx context.Context, runtime *databaseRuntime, errorText string) error {
	if runtime == nil || runtime.database == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	errorText = s.redactForPersistence(ctx, runtime, errorText)
	_, err := runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'error', error = ?, completed_at = COALESCE(completed_at, ?)
		WHERE status = 'running'`,
		errorText,
		now,
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "database is closed") {
		return nil
	}
	return err
}
