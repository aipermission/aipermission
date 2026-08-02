package connectors

import "errors"

// ClassifiedError exposes a stable machine-readable connector failure code
// without coupling the gateway to connector-specific error types.
type ClassifiedError struct {
	Code string
	Err  error
}

func (e *ClassifiedError) Error() string {
	if e == nil || e.Err == nil {
		return "connector action failed"
	}
	return e.Err.Error()
}

func (e *ClassifiedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ClassifyError attaches a stable connector error code to an existing error.
func ClassifyError(code string, err error) error {
	if code == "" || err == nil {
		return err
	}
	return &ClassifiedError{Code: code, Err: err}
}

// ErrorCode returns the stable code attached by a connector, when present.
func ErrorCode(err error) string {
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return classified.Code
	}
	return ""
}
