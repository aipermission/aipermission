package console

import (
	"errors"
	"fmt"
	"time"
)

func (s *managedConsoleSession) unknownCommandObservation(active consoleSessionActiveExec, cause error) (ExecResult, error) {
	return ExecResult{
			SessionID: s.id, Generation: s.generation, Command: active.Command,
			Running: true, DurationMS: time.Since(active.Started).Milliseconds(),
		}, &redactedConsoleError{
			cause:   errors.Join(ErrCommandOutcomeUnknown, cause),
			message: s.redactForPersistence(fmt.Sprintf("%s: %v", ErrCommandOutcomeUnknown, cause)),
		}
}
