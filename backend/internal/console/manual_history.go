package console

import (
	"strings"

	"github.com/aipermission/aipermission/backend/internal/console/manualinput"
	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
)

const (
	maxManualCapturedOutputBytes = 1 << 20
	manualCommandReason          = "manual console command not tracked"
	manualTrackedCommandReason   = "manual console command"
	manualCaptureSuperseded      = "manual_capture_superseded"
	manualPromptNotDetected      = "prompt_not_detected"
	manualSessionClosed          = "session_closed"
	manualActiveExecPaused       = "active_exec_paused"
)

type manualCommandRecord struct {
	manualinput.Record
	StartOffset  int64
	ResumePrompt string
}

type manualInputPreparation struct {
	commands     []manualCommandRecord
	completion   *manualOutputCompletion
	activeUpdate *manualActiveCommandUpdate
}

type manualInputBoundary struct {
	startOffset  int64
	resumePrompt string
}

func (s *managedConsoleSession) prepareManualInput(data string) []manualCommandRecord {
	if data == "" || s == nil || s.manager == nil || s.manager.db == nil {
		return nil
	}
	s.mu.Lock()
	preparation := s.prepareManualInputLocked(data, nil)
	s.mu.Unlock()
	return s.finishManualInputPreparation(preparation)
}

func (s *managedConsoleSession) prepareManualInputLocked(data string, boundary *manualInputBoundary) manualInputPreparation {
	if s.activeExec != nil {
		s.manualInput.Reset()
		return manualInputPreparation{}
	}

	commands := []manualCommandRecord{}
	var completion *manualOutputCompletion
	var activeUpdate *manualActiveCommandUpdate
	s.clearManualPauseIfPromptReturnedLocked()
	if s.manualPause != nil {
		s.manualInput.Reset()
		return manualInputPreparation{}
	}
	if strings.ContainsAny(data, "\r\n") && s.manualActive != nil {
		completion = s.manualOutputCompletionLocked()
	}
	if activeUpdate == nil {
		startOffset := s.rawStreamPositionLocked()
		resumePrompt := terminaltext.LastManualShellPrompt(s.rawTranscript)
		if boundary != nil {
			startOffset = boundary.startOffset
			resumePrompt = boundary.resumePrompt
		}
		observations := []manualinput.Record{}
		for _, command := range s.manualInput.Consume(data) {
			if command.Command != "" {
				observations = append(observations, command)
			}
		}
		for _, observation := range manualinput.Collapse(observations) {
			commands = append(commands, manualCommandRecord{
				Record: observation, StartOffset: startOffset, ResumePrompt: resumePrompt,
			})
		}
		if completion == nil && s.manualActive != nil && len(commands) > 0 {
			if manualActiveIsHistoryRecall(s.manualActive) {
				active := *s.manualActive
				completion = s.downgradeManualOutputCaptureLocked("history_recall_untracked", false)
				s.pauseManualCaptureAfterActiveLocked(active)
				s.manualInput.Reset()
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
	boundary := manualInputBoundary{
		startOffset:  s.rawStreamPositionLocked(),
		resumePrompt: terminaltext.LastManualShellPrompt(s.rawTranscript),
	}
	s.mu.Unlock()
	if err := s.writeInput(data); err != nil {
		return err
	}
	var preparation manualInputPreparation
	if data != "" && s.manager != nil && s.manager.db != nil {
		s.mu.Lock()
		preparation = s.prepareManualInputLocked(data, &boundary)
		s.mu.Unlock()
	}
	commands := s.finishManualInputPreparation(preparation)
	s.persistManualInput(commands)
	return nil
}
