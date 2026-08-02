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
