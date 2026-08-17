package connectors

import "errors"

// ClassifiedError exposes a stable machine-readable connector failure code
// without coupling the gateway to connector-specific error types.
type ClassifiedError struct {
	Code    string
	Status  ResultStatus
	Details map[string]any
	Err     error
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
	return ClassifyActionError(code, "", nil, err)
}

// ClassifyActionError attaches a stable code, lifecycle status, and bounded
// non-secret details to an execution error without coupling core to one
// connector's error types.
func ClassifyActionError(code string, status ResultStatus, details map[string]any, err error) error {
	if code == "" || err == nil {
		return err
	}
	return &ClassifiedError{Code: code, Status: status, Details: cloneErrorDetails(details), Err: err}
}

// ErrorCode returns the stable code attached by a connector, when present.
func ErrorCode(err error) string {
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return classified.Code
	}
	return ""
}

// ErrorStatus returns the connector-provided lifecycle status, when present.
func ErrorStatus(err error) ResultStatus {
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return classified.Status
	}
	return ""
}

// ErrorDetails returns a copy of connector-provided non-secret failure data.
func ErrorDetails(err error) map[string]any {
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return cloneErrorDetails(classified.Details)
	}
	return nil
}

func cloneErrorDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(details))
	for key, value := range details {
		cloned[key] = value
	}
	return cloned
}
