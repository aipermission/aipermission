package console

// Keep error classification available to admission and recovery while the
// display text remains safe after the session's exact redactor is destroyed.
type redactedConsoleError struct {
	cause   error
	message string
}

func (err *redactedConsoleError) Error() string { return err.message }
func (err *redactedConsoleError) Unwrap() error { return err.cause }
