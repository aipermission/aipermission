package serviceboundary

import (
	"context"
	"errors"
)

// SafeError keeps ordinary transport causes but withholds reflected credentials,
// including diagnostics created while parsing remote response headers.
func (boundary *Boundary) SafeError(err error) error {
	if err == nil {
		return nil
	}
	if checked := boundary.CheckMetadata(err.Error()); checked != nil {
		return safeCause(checked, err)
	}
	return err
}

// ReadError discards untrusted body-reader diagnostics, retaining only standard
// cancellation classes needed by HTTP status mapping and callers.
func ReadError(err error) error {
	if err == nil {
		return nil
	}
	return safeCause(errors.New("backup service JSON response could not be read"), err)
}

func safeCause(safe, original error) error {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		if errors.Is(original, cause) {
			safe = errors.Join(safe, cause)
		}
	}
	return safe
}
