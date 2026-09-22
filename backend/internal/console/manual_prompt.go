package console

import "github.com/aipermission/aipermission/backend/internal/console/terminaltext"

func (s *managedConsoleSession) clearManualPauseIfPromptReturnedLocked() {
	if s == nil || s.manualPause == nil {
		return
	}
	startOffset := s.manualPause.StartOffset
	segment, _ := s.rawSegmentLocked(startOffset)
	if segment != "" && terminaltext.ManualTranscriptEndsWithPrompt(segment, s.manualPause.Prompt) {
		s.manualPause = nil
		s.manualInput.reset()
	}
}

func manualReasonPausesCapture(reason string) bool {
	switch reason {
	case "interactive_editor", "interactive_repl", "interactive_tui", "nested_shell", "long_running_stream", "may_prompt":
		return true
	default:
		return false
	}
}

func manualActiveIsHistoryRecall(active *consoleSessionManualCapture) bool {
	if active == nil {
		return false
	}
	return active.CompletionTrackingReason == "history_recall_untracked" || active.Command == "command recalled with arrow key"
}
