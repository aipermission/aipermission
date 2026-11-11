package execution

import "errors"

// Staging-record cleanup is secondary evidence, not the cause of the failed
// remote transfer. Keep it visible without changing primary error matching.
type uploadCleanupError struct {
	primary error
	cleanup error
}

func (err *uploadCleanupError) Error() string {
	return errors.Join(err.primary, err.cleanup).Error()
}

func (err *uploadCleanupError) Unwrap() error { return err.primary }

func withUploadCleanupError(primary, cleanup error) error {
	if cleanup == nil {
		return primary
	}
	return &uploadCleanupError{primary: primary, cleanup: cleanup}
}
