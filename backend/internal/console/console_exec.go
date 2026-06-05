package console

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const restoreTerminalInputCommand = "stty sane 2>/dev/null || stty echo icanon opost 2>/dev/null || true\n"

func (s *managedConsoleSession) execCommand(ctx context.Context, command string) (ExecResult, error) {
	s.execMu.Lock()
	defer s.execMu.Unlock()

	if err := s.waitReady(ctx); err != nil {
		return ExecResult{}, err
	}

	if active := s.activeCommand(); active != nil {
		output, exitCode, completed, err := s.checkCommandResult(active.StartOffset, active.Marker)
		if err != nil {
			s.clearActiveCommand(active.Marker)
			return ExecResult{}, err
		}
		if completed {
			s.restoreTerminalInput()
			return ExecResult{
				SessionID:  s.id,
				Command:    active.Command,
				Output:     output,
				ExitCode:   exitCode,
				DurationMS: time.Since(active.Started).Milliseconds(),
			}, ErrCommandActive
		}
		return ExecResult{
			SessionID:  s.id,
			Command:    active.Command,
			Output:     output,
			Running:    true,
			DurationMS: time.Since(active.Started).Milliseconds(),
		}, ErrCommandActive
	}

	s.closeManualOutputCapture(manualActiveExecPaused)

	started := time.Now()
	marker := fmt.Sprintf("__AIPERMISSION_EXIT_%d_%d__", s.id, started.UnixNano())
	s.mu.Lock()
	startOffset := len(s.rawTranscript)
	s.mu.Unlock()

	s.setActiveCommand(consoleSessionActiveExec{
		Command:     command,
		Marker:      marker,
		StartOffset: startOffset,
		Started:     started,
	})

	payload := consoleExecPayload(command, marker)
	s.appendDisplayOutput(formatAutomationCommand(command))
	if err := s.writeInput("__aipermission_saved_ps2=${PS2-}\nPS2=\nstty -echo 2>/dev/null || true\n"); err != nil {
		s.clearActiveCommand(marker)
		return ExecResult{}, err
	}
	time.Sleep(80 * time.Millisecond)
	if err := s.writeInput(payload); err != nil {
		s.clearActiveCommand(marker)
		return ExecResult{}, err
	}

	output, exitCode, err := s.waitForCommandResult(ctx, startOffset, marker)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return ExecResult{
				SessionID:  s.id,
				Command:    command,
				Output:     output,
				Running:    true,
				DurationMS: time.Since(started).Milliseconds(),
			}, nil
		}
		s.clearActiveCommand(marker)
		return ExecResult{}, err
	}
	s.clearActiveCommand(marker)
	s.restoreTerminalInput()

	return ExecResult{
		SessionID:  s.id,
		Command:    command,
		Output:     output,
		ExitCode:   exitCode,
		DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func (s *managedConsoleSession) waitActiveCommand(ctx context.Context) (ExecResult, error) {
	active := s.activeCommand()
	if active == nil {
		return ExecResult{}, fmt.Errorf("no active command")
	}
	output, exitCode, err := s.waitForCommandResult(ctx, active.StartOffset, active.Marker)
	if err != nil {
		return ExecResult{}, err
	}
	s.clearActiveCommand(active.Marker)
	s.restoreTerminalInput()
	return ExecResult{
		SessionID:  s.id,
		Command:    active.Command,
		Output:     output,
		ExitCode:   exitCode,
		DurationMS: time.Since(active.Started).Milliseconds(),
	}, nil
}

func (s *managedConsoleSession) interruptActiveCommand(ctx context.Context) error {
	active := s.activeCommand()
	if active == nil {
		return nil
	}
	if err := s.writeInput("\x03"); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(250 * time.Millisecond):
	}
	s.clearActiveCommand(active.Marker)
	s.restoreTerminalInput()
	return nil
}

func (s *managedConsoleSession) restoreTerminalInput() {
	_ = s.writeInput(restoreTerminalInputCommand)
}

func (s *managedConsoleSession) activeCommand() *consoleSessionActiveExec {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeExec == nil {
		return nil
	}
	active := *s.activeExec
	return &active
}

func (s *managedConsoleSession) setActiveCommand(active consoleSessionActiveExec) {
	s.mu.Lock()
	s.activeExec = &active
	s.mu.Unlock()
}

func (s *managedConsoleSession) clearActiveCommand(marker string) {
	s.mu.Lock()
	if s.activeExec != nil && s.activeExec.Marker == marker {
		s.activeExec = nil
		s.filterUntil = time.Now().Add(750 * time.Millisecond)
	}
	s.mu.Unlock()
}

func (s *managedConsoleSession) waitReady(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, _ := s.snapshot()
		switch status {
		case "connected":
			return nil
		case "error", "closed":
			s.mu.Lock()
			errText := s.errText
			s.mu.Unlock()
			if errText == "" {
				errText = "console session is not active"
			}
			return errors.New(errText)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return fmt.Errorf("console session closed")
		case <-ticker.C:
		}
	}
}

func (s *managedConsoleSession) waitForCommandResult(ctx context.Context, startOffset int, marker string) (string, int, error) {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		output, exitCode, completed, err := s.checkCommandResult(startOffset, marker)
		if err != nil {
			return output, exitCode, err
		}
		if completed {
			return output, exitCode, nil
		}
		select {
		case <-ctx.Done():
			return output, 1, ctx.Err()
		case <-s.ctx.Done():
			return output, 1, fmt.Errorf("console session closed")
		case <-ticker.C:
		}
	}
}

func (s *managedConsoleSession) checkCommandResult(startOffset int, marker string) (string, int, bool, error) {
	s.mu.Lock()
	transcript := s.rawTranscript
	status := s.status
	errText := s.errText
	s.mu.Unlock()
	if startOffset > len(transcript) {
		startOffset = 0
	}
	segment := transcript[startOffset:]
	markerNeedle := "\n" + marker + ":"
	markerIndex := strings.Index(segment, markerNeedle)
	if markerIndex >= 0 {
		output := segment[:markerIndex]
		afterMarker := segment[markerIndex+len(markerNeedle):]
		lineEnd := strings.IndexAny(afterMarker, "\r\n")
		exitText := afterMarker
		if lineEnd >= 0 {
			exitText = afterMarker[:lineEnd]
		}
		exitCode, err := strconv.Atoi(strings.TrimSpace(exitText))
		if err != nil {
			exitCode = 1
		}
		return output, exitCode, true, nil
	}
	if status == "error" || status == "closed" {
		if errText == "" {
			errText = "console session closed before command completed"
		}
		return segment, 1, false, errors.New(errText)
	}
	return segment, 1, false, nil
}

func consoleExecPayload(command string, marker string) string {
	delimiter := marker + "_SCRIPT"
	for strings.Contains(command, "\n"+delimiter+"\n") {
		delimiter += "_X"
	}
	return fmt.Sprintf("/bin/sh <<'%s'\n(\n%s\n) </dev/null\n__aipermission_exit=$?; PS2=${__aipermission_saved_ps2-}; unset __aipermission_saved_ps2; stty sane 2>/dev/null || stty echo icanon opost 2>/dev/null || true; printf '\\n%s:%%s\\n' \"$__aipermission_exit\"; unset __aipermission_exit\n%s\n", delimiter, command, marker, delimiter)
}
