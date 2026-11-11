package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestUploadCleanupDeadlineDoesNotBecomeTransferDeadline(t *testing.T) {
	err := withUploadCleanupError(context.Canceled, fmt.Errorf("clear remote staging record: %w", context.DeadlineExceeded))
	if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup changed primary classification: %v", err)
	}
	if !strings.Contains(err.Error(), "clear remote staging record: context deadline exceeded") {
		t.Fatalf("cleanup evidence lost: %v", err)
	}
}

func TestUploadCleanupKeepsPrimaryTimeoutAndUnknownOutcome(t *testing.T) {
	deadline := withUploadCleanupError(context.DeadlineExceeded, context.Canceled)
	if !errors.Is(deadline, context.DeadlineExceeded) || errors.Is(deadline, context.Canceled) {
		t.Fatalf("primary timeout changed: %v", deadline)
	}
	primary := connectors.ClassifyOutcomeUnknown("remote_commit", nil, errors.New("reply lost"))
	err := withUploadCleanupError(primary, context.DeadlineExceeded)
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("unknown outcome or cleanup evidence lost: %v", err)
	}
	if withUploadCleanupError(nil, nil) != nil || withUploadCleanupError(primary, nil) != primary {
		t.Fatal("empty cleanup changed primary result")
	}
}
