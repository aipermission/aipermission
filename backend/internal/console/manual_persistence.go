package console

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/history"
)

func (s *managedConsoleSession) insertManualCommand(command manualCommandRecord) error {
	if command.Command == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	storedCommand := s.redactForPersistence(command.Command)
	storedReason := s.redactForPersistence(manualCommandReason)
	status := "untracked"
	completedAt := sql.NullString{String: now, Valid: true}
	if command.TrackOutput {
		storedReason = s.redactForPersistence(manualTrackedCommandReason)
		status = "running"
		completedAt = sql.NullString{}
		if err := s.closeStaleManualRunningRows(0, manualCaptureSuperseded); err != nil {
			return err
		}
	}
	trackingReason := s.redactForPersistence(command.TrackingReason)
	var requestID int64
	err := s.withManualHistoryTransaction(context.Background(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(context.Background(), `
				INSERT INTO command_requests (runtime_id, source, command, encrypted_command, reason, status, tracking_reason, stdout, stderr, session_id, created_at, completed_at)
				VALUES (?, 'manual', ?, '', ?, ?, ?, '', '', ?, ?, ?)`,
			s.runtimeID,
			storedCommand,
			storedReason,
			status,
			trackingReason,
			s.id,
			now,
			completedAt,
		)
		if err != nil {
			return fmt.Errorf("insert manual command history: %w", err)
		}
		requestID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read manual command history id: %w", err)
		}
		return history.SyncCommandRequestWithExecutor(context.Background(), tx, requestID)
	})
	if err != nil {
		return err
	}
	if command.TrackOutput {
		completion := s.setManualOutputCapture(consoleSessionManualCapture{
			RequestID:                requestID,
			Command:                  command.Command,
			StartOffset:              command.StartOffset,
			ResumePrompt:             command.ResumePrompt,
			Started:                  time.Now(),
			CompletionTrackingReason: command.CompletionTrackingReason,
		})
		if completion != nil {
			go s.finishManualOutputCapture(completion)
		}
	} else {
		s.pauseManualCaptureAfterCommand(command)
	}
	return nil
}

func (s *managedConsoleSession) withManualHistoryTransaction(ctx context.Context, mutate func(*sql.Tx) error) error {
	if s == nil || s.manager == nil || s.manager.db == nil {
		return fmt.Errorf("manual command history is not configured")
	}
	tx, err := s.manager.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin manual command history transaction: %w", err)
	}
	defer tx.Rollback()
	if err := mutate(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit manual command history transaction: %w", err)
	}
	return nil
}
