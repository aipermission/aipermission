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

// ClassifyOutcomeUnknown applies the shared fail-closed contract used after a
// mutation may have crossed an external dispatch boundary.
func ClassifyOutcomeUnknown(stage string, details map[string]any, err error) error {
	if err == nil {
		return nil
	}
	details = cloneErrorDetails(details)
	if details == nil {
		details = map[string]any{}
	}
	details["dispatch_stage"] = stage
	details["retry_safe"] = false
	return ClassifyActionError("outcome_unknown", ResultOutcomeUnknown, details, err)
}

// OutcomeUnknownResult is the result-form equivalent of
// ClassifyOutcomeUnknown for connectors whose execution API returns failures
// as ActionResult values.
func OutcomeUnknownResult(stage string, details map[string]any, err error) ActionResult {
	details = cloneErrorDetails(details)
	if details == nil {
		details = map[string]any{}
	}
	details["dispatch_stage"] = stage
	details["retry_safe"] = false
	message := "mutation outcome is unknown after dispatch"
	if err != nil {
		message = err.Error()
	}
	return ActionResult{Status: ResultOutcomeUnknown, Output: details, Error: message, DisplayText: message}
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
