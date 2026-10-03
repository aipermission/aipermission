package console

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
)

const restoreTerminalInputCommand = "stty sane 2>/dev/null || stty echo icanon opost 2>/dev/null || true\n"

func (s *managedConsoleSession) execCommand(
	ctx context.Context,
	command string,
	authorizedWrite func(func() error) error,
) (ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return ExecResult{}, err
	}
	s.execMu.Lock()
	defer s.execMu.Unlock()

	if err := s.waitReady(ctx); err != nil {
		return ExecResult{}, err
	}

	s.inputMu.Lock()
	if err := ctx.Err(); err != nil {
		s.inputMu.Unlock()
		return ExecResult{}, err
	}
	if active := s.activeCommand(); active != nil {
		output, exitCode, completed, err := s.checkCommandResult(active.StartOffset, active.Marker)
		s.inputMu.Unlock()
		if err != nil {
			return ExecResult{}, ErrCommandActive
		}
		if completed {
			s.restoreTerminalInput()
			return ExecResult{
				SessionID:  s.id,
				Generation: s.generation,
				Command:    active.Command,
				Output:     output,
				ExitCode:   exitCode,
				DurationMS: time.Since(active.Started).Milliseconds(),
			}, ErrCommandActive
		}
		return ExecResult{
			SessionID:  s.id,
			Generation: s.generation,
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
	startOffset := s.rawStreamPositionLocked()
	s.mu.Unlock()

	active := consoleSessionActiveExec{
		Command:     command,
		Marker:      marker,
		StartOffset: startOffset,
		Started:     started,
	}
	s.setActiveCommand(active)
	s.inputMu.Unlock()

	payloadAttempted := false
	writeCommand := func() error {
		s.inputMu.Lock()
		defer s.inputMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.writeInput(terminaltext.ExecPrelude); err != nil {
			return err
		}
		timer := time.NewTimer(120 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			s.restoreTerminalInputLocked()
			return ctx.Err()
		case <-timer.C:
		}
		if err := ctx.Err(); err != nil {
			s.restoreTerminalInputLocked()
			return err
		}
		var err error
		payloadAttempted, err = s.writeInputAttempt(terminaltext.ExecPayload(command, marker))
		return err
	}
	var writeErr error
	if authorizedWrite != nil {
		writeErr = authorizedWrite(writeCommand)
	} else {
		writeErr = writeCommand()
	}
	if writeErr != nil {
		if payloadAttempted {
			return s.unknownCommandObservation(active, writeErr)
		}
		s.clearActiveCommandWithInputAdmission(marker)
		return ExecResult{}, writeErr
	}
	if err := s.appendDisplayOutput(ctx, terminaltext.FormatAutomationCommand(command)); err != nil {
		return s.unknownCommandObservation(active, err)
	}

	output, exitCode, err := s.waitForCommandResult(ctx, startOffset, marker)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return ExecResult{
				SessionID:  s.id,
				Generation: s.generation,
				Command:    command,
				Output:     output,
				Running:    true,
				DurationMS: time.Since(started).Milliseconds(),
			}, nil
		}
		return s.unknownCommandObservation(active, err)
	}
	s.restoreTerminalInputAndClear(marker)

	return ExecResult{
		SessionID:  s.id,
		Generation: s.generation,
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
	s.restoreTerminalInputAndClear(active.Marker)
	return ExecResult{
		SessionID:  s.id,
		Generation: s.generation,
		Command:    active.Command,
		Output:     output,
		ExitCode:   exitCode,
		DurationMS: time.Since(active.Started).Milliseconds(),
	}, nil
}

func (s *managedConsoleSession) interruptActiveCommand(ctx context.Context) error {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
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
	s.restoreTerminalInputLocked()
	s.clearActiveCommand(active.Marker)
	return nil
}

func (s *managedConsoleSession) restoreTerminalInput() {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	s.restoreTerminalInputLocked()
}

func (s *managedConsoleSession) restoreTerminalInputLocked() {
	_ = s.writeInput(restoreTerminalInputCommand)
}

func (s *managedConsoleSession) restoreTerminalInputAndClear(marker string) {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	s.restoreTerminalInputLocked()
	s.clearActiveCommand(marker)
}

func (s *managedConsoleSession) clearActiveCommandWithInputAdmission(marker string) {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	s.clearActiveCommand(marker)
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
		if err := ctx.Err(); err != nil {
			return err
		}
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

func (s *managedConsoleSession) waitForCommandResult(ctx context.Context, startOffset int64, marker string) (string, int, error) {
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

func (s *managedConsoleSession) checkCommandResult(startOffset int64, marker string) (string, int, bool, error) {
	s.mu.Lock()
	transcript := s.rawTranscript
	baseOffset := s.rawBaseOffset
	status := s.status
	errText := s.errText
	s.mu.Unlock()
	segment, truncated := rawTranscriptSegment(transcript, baseOffset, startOffset)
	output, exitCode, found, complete, err := terminaltext.CommandExitMarker(segment, marker, truncated)
	if found {
		return output, exitCode, complete, err
	}
	if status == "error" || status == "closed" {
		if errText == "" {
			errText = "console session closed before command completed"
		}
		return segment, 1, false, errors.New(errText)
	}
	return segment, 1, false, nil
}
