package console

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/history"
)

type manualOutputCompletion struct {
	RequestID       int64
	Status          string
	Stdout          string
	OutputTruncated bool
	TrackingReason  string
	Error           string
}

type manualActiveCommandUpdate struct {
	RequestID      int64
	Command        string
	TrackingReason string
	Downgrade      bool
}

func (s *managedConsoleSession) appendManualActiveCommandsLocked(commands []manualCommandRecord) *manualActiveCommandUpdate {
	if s.manualActive == nil || len(commands) == 0 {
		return nil
	}
	active := *s.manualActive
	parts := []string{strings.TrimSpace(active.Command)}
	trackOutput := true
	reason := "manual_output_not_tracked"
	for _, command := range commands {
		if command.Command == "" {
			continue
		}
		parts = append(parts, command.Command)
		if !command.TrackOutput {
			trackOutput = false
			reason = "compound_command"
		}
	}
	combined := strings.TrimSpace(strings.Join(parts, "\n"))
	if combined == "" {
		return nil
	}
	if len(combined) > maxManualCommandPreviewBytes {
		combined = manualCommandPreview(combined, true)
		trackOutput = false
		reason = "command_preview_truncated"
	}
	active.Command = combined
	if trackOutput {
		s.manualActive = &active
	} else {
		s.manualActive = nil
	}
	return &manualActiveCommandUpdate{
		RequestID:      active.RequestID,
		Command:        combined,
		TrackingReason: reason,
		Downgrade:      !trackOutput,
	}
}

func (s *managedConsoleSession) pauseManualCaptureAfterCommand(command manualCommandRecord) {
	if !manualReasonPausesCapture(command.TrackingReason) {
		return
	}
	s.mu.Lock()
	s.manualPause = &consoleSessionManualPause{
		Prompt:      command.ResumePrompt,
		Reason:      command.TrackingReason,
		StartOffset: command.StartOffset,
	}
	s.manualInput.reset()
	s.mu.Unlock()
}

func (s *managedConsoleSession) pauseManualCaptureAfterActiveLocked(active consoleSessionManualCapture) {
	s.manualPause = &consoleSessionManualPause{
		Prompt:      active.ResumePrompt,
		Reason:      active.CompletionTrackingReason,
		StartOffset: active.StartOffset,
	}
}

func (s *managedConsoleSession) setManualOutputCapture(capture consoleSessionManualCapture) *manualOutputCompletion {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manualActive = &capture
	return s.manualOutputCompletionLocked()
}

func (s *managedConsoleSession) updateManualActiveCommand(update *manualActiveCommandUpdate) {
	if update == nil || s == nil || s.manager == nil || s.manager.db == nil {
		return
	}
	command := s.redactForPersistence(update.Command)
	trackingReason := s.redactForPersistence(update.TrackingReason)
	if update.Downgrade {
		now := time.Now().UTC().Format(time.RFC3339)
		err := s.withManualHistoryTransaction(context.Background(), func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(context.Background(), `
					UPDATE command_requests
					SET command = ?, status = 'untracked', tracking_reason = ?, completed_at = COALESCE(completed_at, ?)
					WHERE id = ? AND status = 'running'`,
				command,
				trackingReason,
				now,
				update.RequestID,
			); err != nil {
				return err
			}
			return history.SyncCommandRequestWithExecutor(context.Background(), tx, update.RequestID)
		})
		if err != nil {
			logConsolePersistError("manual_history_update", s.id, err)
		}
		if err := s.closeStaleManualRunningRows(update.RequestID, update.TrackingReason); err != nil {
			logConsolePersistError("manual_history_stale", s.id, err)
		}
		return
	}
	err := s.withManualHistoryTransaction(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `
				UPDATE command_requests
				SET command = ?
				WHERE id = ? AND status = 'running'`,
			command,
			update.RequestID,
		); err != nil {
			return err
		}
		return history.SyncCommandRequestWithExecutor(context.Background(), tx, update.RequestID)
	})
	if err != nil {
		logConsolePersistError("manual_history_update", s.id, err)
	}
}

func (s *managedConsoleSession) manualOutputCompletionLocked() *manualOutputCompletion {
	if s.manualActive == nil {
		return nil
	}
	active := *s.manualActive
	startOffset := active.StartOffset
	truncated := false
	if startOffset > len(s.rawTranscript) {
		startOffset = 0
		truncated = true
	}
	segment := s.rawTranscript[startOffset:]
	if !manualSegmentHasPrompt(segment) {
		return nil
	}
	if manualActiveIsHistoryRecall(&active) && strings.TrimSpace(active.ResumePrompt) != "" && !manualTranscriptEndsWithPrompt(segment, active.ResumePrompt) {
		return nil
	}
	stdout, outputTruncated := manualCapturedOutput(segment, active.Command)
	truncated = truncated || outputTruncated
	status := "completed"
	errorText := ""
	if strings.Contains(segment, "^C") {
		status = "canceled"
		errorText = "manual command interrupted"
	}
	trackingReason := active.CompletionTrackingReason
	if strings.TrimSpace(trackingReason) == "" {
		trackingReason = "exit_code_unavailable"
	}
	s.manualActive = nil
	return &manualOutputCompletion{
		RequestID:       active.RequestID,
		Status:          status,
		Stdout:          stdout,
		OutputTruncated: truncated,
		TrackingReason:  trackingReason,
		Error:           errorText,
	}
}

func (s *managedConsoleSession) manualActiveHasOutputLocked() bool {
	if s.manualActive == nil {
		return false
	}
	active := *s.manualActive
	startOffset := active.StartOffset
	if startOffset > len(s.rawTranscript) {
		startOffset = 0
	}
	stdout, _ := manualCapturedOutput(s.rawTranscript[startOffset:], active.Command)
	return strings.TrimSpace(PlainOutput(stdout)) != ""
}

func (s *managedConsoleSession) downgradeManualOutputCaptureLocked(reason string, captureOutput bool) *manualOutputCompletion {
	if s.manualActive == nil {
		return nil
	}
	active := *s.manualActive
	startOffset := active.StartOffset
	truncated := false
	if startOffset > len(s.rawTranscript) {
		startOffset = 0
		truncated = true
	}
	stdout := ""
	outputTruncated := false
	if captureOutput {
		stdout, outputTruncated = manualCapturedOutput(s.rawTranscript[startOffset:], active.Command)
	}
	s.manualActive = nil
	return &manualOutputCompletion{
		RequestID:       active.RequestID,
		Status:          "untracked",
		Stdout:          stdout,
		OutputTruncated: truncated || outputTruncated,
		TrackingReason:  reason,
	}
}

func (s *managedConsoleSession) finishManualOutputCapture(completion *manualOutputCompletion) {
	if completion == nil || s == nil || s.manager == nil || s.manager.db == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	stdout := s.redactForPersistence(PlainOutput(completion.Stdout))
	errorText := s.redactForPersistence(completion.Error)
	trackingReason := s.redactForPersistence(completion.TrackingReason)
	outputTruncated := 0
	if completion.OutputTruncated {
		outputTruncated = 1
	}
	err := s.withManualHistoryTransaction(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `
				UPDATE command_requests
				SET status = ?, stdout = ?, stderr = '', tracking_reason = ?, output_truncated = ?, error = ?, completed_at = ?
				WHERE id = ? AND source = 'manual'`,
			completion.Status,
			stdout,
			trackingReason,
			outputTruncated,
			errorText,
			now,
			completion.RequestID,
		); err != nil {
			return err
		}
		return history.SyncCommandRequestWithExecutor(context.Background(), tx, completion.RequestID)
	})
	if err != nil {
		logConsolePersistError("manual_history_finish", s.id, err)
	}
	if err := s.closeStaleManualRunningRows(completion.RequestID, manualCaptureSuperseded); err != nil {
		logConsolePersistError("manual_history_stale", s.id, err)
	}
}

func (s *managedConsoleSession) closeManualOutputCapture(reason string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.manualPause = nil
	completion := s.manualOutputCompletionLocked()
	if completion == nil {
		completion = s.downgradeManualOutputCaptureLocked(reason, true)
	}
	s.mu.Unlock()
	s.finishManualOutputCapture(completion)
}

func (s *managedConsoleSession) closeStaleManualRunningRows(exceptID int64, reason string) error {
	if s == nil || s.manager == nil || s.manager.db == nil || s.id < 1 {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = manualCaptureSuperseded
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return s.withManualHistoryTransaction(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(context.Background(), `
				SELECT id
				FROM command_requests
				WHERE source = 'manual'
					AND session_id = ?
					AND status = 'running'
					AND (? = 0 OR id <> ?)`,
			s.id,
			exceptID,
			exceptID,
		)
		if err != nil {
			return fmt.Errorf("list stale manual command rows: %w", err)
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan stale manual command row: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate stale manual command rows: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close stale manual command rows: %w", err)
		}
		if _, err := tx.ExecContext(context.Background(), `
				UPDATE command_requests
				SET status = 'untracked', tracking_reason = ?, completed_at = COALESCE(completed_at, ?)
				WHERE source = 'manual'
					AND session_id = ?
					AND status = 'running'
					AND (? = 0 OR id <> ?)`,
			s.redactForPersistence(reason), now, s.id, exceptID, exceptID,
		); err != nil {
			return err
		}
		for _, id := range ids {
			if err := history.SyncCommandRequestWithExecutor(context.Background(), tx, id); err != nil {
				return err
			}
		}
		return nil
	})
}
