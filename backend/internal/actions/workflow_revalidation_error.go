package actions

import "fmt"

type revalidationError struct {
	message string
	cause   error
}

func (err *revalidationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrConnectorAuthorizationChanged, err.message)
}

func (err *revalidationError) Unwrap() []error {
	return []error{ErrConnectorAuthorizationChanged, err.cause}
}

// Operational failures stay inspectable without putting raw dependency errors
// into the permission-denial text returned to callers.
func revalidationFailure(message string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrConnectorAuthorizationChanged, message)
	}
	return &revalidationError{message: message, cause: cause}
}
