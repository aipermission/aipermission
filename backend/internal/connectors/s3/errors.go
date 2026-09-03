package s3connector

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type s3StatusError struct {
	status  int
	code    string
	message string
}

type s3TransportError struct {
	stage string
	err   error
}

func (err *s3TransportError) Error() string { return err.err.Error() }
func (err *s3TransportError) Unwrap() error { return err.err }

func (err *s3StatusError) Error() string {
	return err.message
}

func (err *s3StatusError) HTTPStatusCode() int { return err.status }

func s3HTTPError(status int, data []byte) error {
	var serviceError struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	_ = xml.Unmarshal(data, &serviceError)
	return classifyS3ServiceError(status, serviceError.Code, firstNonEmpty(serviceError.Message, strings.TrimSpace(string(data))))
}

func classifyS3ServiceError(status int, code string, detail string) error {
	message := strings.TrimSpace(detail)
	if len(message) > 800 {
		message = message[:800] + "...[truncated]"
	}
	if message == "" {
		message = http.StatusText(status)
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		message = fmt.Sprintf("s3 authentication or permission failed: %s", message)
	case http.StatusNotFound:
		message = fmt.Sprintf("s3 object or bucket not found: %s", message)
	default:
		message = fmt.Sprintf("s3 request failed with HTTP %d: %s", status, message)
	}
	err := &s3StatusError{status: status, code: strings.TrimSpace(code), message: message}
	if status == http.StatusPreconditionFailed || (status == http.StatusConflict && err.code == "ConditionalRequestConflict") {
		return connectors.ClassifyError("precondition_failed", err)
	}
	if status == http.StatusNotFound {
		return connectors.ClassifyError("not_found", err)
	}
	return err
}

func classifyConditionalS3Error(err error, headers http.Header) error {
	if err == nil || (headers.Get("If-Match") == "" && headers.Get("If-None-Match") == "") {
		return err
	}
	var statusErr *s3StatusError
	if errors.As(err, &statusErr) && statusErr.status == http.StatusNotFound && isConditionalObjectNotFound(statusErr.code) {
		return connectors.ClassifyError("precondition_failed", err)
	}
	return err
}

func classifyS3MutationError(err error, headers http.Header) error {
	err = classifyConditionalS3Error(err, headers)
	if err == nil || connectors.ErrorCode(err) != "" {
		return err
	}
	var transportErr *s3TransportError
	if errors.As(err, &transportErr) {
		if transportErr.stage == "before_dispatch" {
			return err
		}
		return unknownS3MutationError(transportErr.stage, err)
	}
	var statusErr *s3StatusError
	if errors.As(err, &statusErr) && (statusErr.status == http.StatusRequestTimeout || statusErr.status >= http.StatusInternalServerError) {
		return unknownS3MutationError("response_status", err)
	}
	return err
}

func unknownS3MutationError(stage string, err error) error {
	return connectors.ClassifyActionError(
		"outcome_unknown",
		connectors.ResultOutcomeUnknown,
		map[string]any{"dispatch_stage": stage},
		fmt.Errorf("s3 mutation outcome is unknown after %s failure: %w", stage, err),
	)
}

func isConditionalObjectNotFound(code string) bool {
	switch strings.TrimSpace(code) {
	case "", "NotFound", "NoSuchKey", "NoSuchVersion":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func classifyS3TestError(err error) connectors.TestStatus {
	if err == nil {
		return connectors.TestOK
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "authentication") || strings.Contains(message, "permission") || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized"):
		return connectors.TestFailedAuth
	case strings.Contains(message, "no such host") || strings.Contains(message, "connection refused") || strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded") || strings.Contains(message, "network"):
		return connectors.TestFailedNetwork
	case strings.Contains(message, "tls") || strings.Contains(message, "certificate"):
		return connectors.TestFailedTLS
	default:
		return connectors.TestUnknownError
	}
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *s3StatusError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusNotFound
	}
	return false
}
