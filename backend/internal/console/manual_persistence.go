package console

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
		s.closeStaleManualRunningRows(0, manualCaptureSuperseded)
	}
	trackingReason := s.redactForPersistence(command.TrackingReason)
	result, err := s.manager.db.ExecContext(context.Background(), `
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
	requestID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read manual command history id: %w", err)
	}
	s.syncManualHistory(requestID)
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
