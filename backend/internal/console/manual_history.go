package console

import (
	"strings"

	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
)

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

type manualInputPreparation struct {
	commands     []manualCommandRecord
	completion   *manualOutputCompletion
	activeUpdate *manualActiveCommandUpdate
}

func (s *managedConsoleSession) prepareManualInput(data string) []manualCommandRecord {
	if data == "" || s == nil || s.manager == nil || s.manager.db == nil {
		return nil
	}
	s.mu.Lock()
	preparation := s.prepareManualInputLocked(data)
	s.mu.Unlock()
	return s.finishManualInputPreparation(preparation)
}

func (s *managedConsoleSession) prepareManualInputLocked(data string) manualInputPreparation {
	if s.activeExec != nil {
		s.manualInput.reset()
		return manualInputPreparation{}
	}

	commands := []manualCommandRecord{}
	var completion *manualOutputCompletion
	var activeUpdate *manualActiveCommandUpdate
	s.clearManualPauseIfPromptReturnedLocked()
	if s.manualPause != nil {
		s.manualInput.reset()
		return manualInputPreparation{}
	}
	if strings.ContainsAny(data, "\r\n") && s.manualActive != nil {
		completion = s.manualOutputCompletionLocked()
	}
	if activeUpdate == nil {
		startOffset := s.rawStreamPositionLocked()
		resumePrompt := terminaltext.LastManualShellPrompt(s.rawTranscript)
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
	return manualInputPreparation{commands: commands, completion: completion, activeUpdate: activeUpdate}
}

func (s *managedConsoleSession) finishManualInputPreparation(preparation manualInputPreparation) []manualCommandRecord {
	if preparation.completion != nil {
		s.finishManualOutputCapture(preparation.completion)
	}
	if preparation.activeUpdate != nil {
		s.updateManualActiveCommand(preparation.activeUpdate)
	}
	return preparation.commands
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

func (s *managedConsoleSession) submitManualInput(data string) error {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	s.mu.Lock()
	if s.activeExec != nil {
		s.mu.Unlock()
		return ErrCommandActive
	}
	if err := s.writeInputLocked(data); err != nil {
		s.mu.Unlock()
		return err
	}
	var preparation manualInputPreparation
	if data != "" && s.manager != nil && s.manager.db != nil {
		preparation = s.prepareManualInputLocked(data)
	}
	s.mu.Unlock()
	commands := s.finishManualInputPreparation(preparation)
	s.persistManualInput(commands)
	return nil
}
