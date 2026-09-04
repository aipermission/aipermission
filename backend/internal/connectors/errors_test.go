package connectors

import (
	"errors"
	"testing"
)

func TestClassifiedErrorPreservesCodeAndCause(t *testing.T) {
	cause := errors.New("unsupported operation")
	err := ClassifyError("operation_unsupported", cause)
	if ErrorCode(err) != "operation_unsupported" {
		t.Fatalf("unexpected error code %q", ErrorCode(err))
	}
	if !errors.Is(err, cause) {
		t.Fatal("classified error did not preserve its cause")
	}
}

func TestClassifiedErrorIgnoresEmptyInputs(t *testing.T) {
	cause := errors.New("plain error")
	if got := ClassifyError("", cause); got != cause {
		t.Fatal("empty code should preserve the original error")
	}
	if got := ClassifyError("operation_unsupported", nil); got != nil {
		t.Fatal("nil error should remain nil")
	}
	if code := ErrorCode(cause); code != "" {
		t.Fatalf("plain error returned code %q", code)
	}
}

func TestClassifiedActionErrorCopiesStatusAndDetails(t *testing.T) {
	details := map[string]any{"retry_safe": false}
	err := ClassifyActionError("outcome_unknown", ResultOutcomeUnknown, details, errors.New("uncertain outcome"))
	details["retry_safe"] = true
	if ErrorStatus(err) != ResultOutcomeUnknown {
		t.Fatalf("unexpected error status %q", ErrorStatus(err))
	}
	returned := ErrorDetails(err)
	if returned["retry_safe"] != false {
		t.Fatalf("classified details changed with caller map: %#v", returned)
	}
	returned["retry_safe"] = true
	if ErrorDetails(err)["retry_safe"] != false {
		t.Fatal("returned error details mutated classified state")
	}
}

func TestOutcomeUnknownHelpersEnforceSharedRetryContract(t *testing.T) {
	cause := errors.New("connection reset")
	err := ClassifyOutcomeUnknown("request_body", map[string]any{"cleanup_required": true}, cause)
	if ErrorCode(err) != "outcome_unknown" || ErrorStatus(err) != ResultOutcomeUnknown || !errors.Is(err, cause) {
		t.Fatalf("classified outcome = code %q status %q error %v", ErrorCode(err), ErrorStatus(err), err)
	}
	details := ErrorDetails(err)
	if details["dispatch_stage"] != "request_body" || details["retry_safe"] != false || details["cleanup_required"] != true {
		t.Fatalf("classified outcome details = %#v", details)
	}

	result := OutcomeUnknownResult("response_body", nil, cause)
	output, ok := result.Output.(map[string]any)
	if !ok || result.Status != ResultOutcomeUnknown || output["dispatch_stage"] != "response_body" || output["retry_safe"] != false {
		t.Fatalf("outcome result = %#v", result)
	}
}
