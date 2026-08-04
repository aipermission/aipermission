package console

import "strings"

const (
	maxManualCommandBufferBytes  = 8192
	maxManualCommandPreviewBytes = 2000
	maxManualCapturedOutputBytes = 1 << 20
	manualCommandReason          = "manual console command not tracked"
	manualTrackedCommandReason   = "manual console command"
	manualCaptureSuperseded      = "manual_capture_superseded"
	manualPromptNotDetected      = "prompt_not_detected"
	manualSessionClosed          = "session_closed"
	manualActiveExecPaused       = "active_exec_paused"
)

func (s *managedConsoleSession) prepareManualInput(data string) []manualCommandRecord {
	if data == "" || s == nil || s.manager == nil || s.manager.db == nil {
		return nil
	}
	if s.activeCommand() != nil {
		s.mu.Lock()
		s.manualInput.reset()
		s.mu.Unlock()
		return nil
	}

	commands := []manualCommandRecord{}
	var completion *manualOutputCompletion
	var activeUpdate *manualActiveCommandUpdate
	s.mu.Lock()
	s.clearManualPauseIfPromptReturnedLocked()
	if s.manualPause != nil {
		s.manualInput.reset()
		s.mu.Unlock()
		return nil
	}
	if strings.ContainsAny(data, "\r\n") && s.manualActive != nil {
		completion = s.manualOutputCompletionLocked()
	}
	if activeUpdate == nil {
		startOffset := len(s.rawTranscript)
		resumePrompt := lastManualShellPrompt(s.rawTranscript)
		for _, command := range s.manualInput.consume(data) {
			if command.Command != "" {
				command.StartOffset = startOffset
				command.ResumePrompt = resumePrompt
				commands = append(commands, command)
			}
		}
		commands = collapseManualCommandRecords(commands)
		if completion == nil && s.manualActive != nil && len(commands) > 0 {
			if manualActiveIsHistoryRecall(s.manualActive) {
				active := *s.manualActive
				completion = s.downgradeManualOutputCaptureLocked("history_recall_untracked", false)
				s.pauseManualCaptureAfterActiveLocked(active)
				s.manualInput.reset()
			} else {
				activeUpdate = s.appendManualActiveCommandsLocked(commands)
			}
			commands = nil
		}
	}
	s.mu.Unlock()
	if completion != nil {
		s.finishManualOutputCapture(completion)
	}
	if activeUpdate != nil {
		s.updateManualActiveCommand(activeUpdate)
	}
	return commands
}

func (s *managedConsoleSession) persistManualInput(commands []manualCommandRecord) {
	for _, command := range commands {
		if err := s.insertManualCommand(command); err != nil {
			logConsolePersistError("manual_history", s.id, err)
		}
	}
}

func (s *managedConsoleSession) recordManualInput(data string) {
	s.persistManualInput(s.prepareManualInput(data))
}
