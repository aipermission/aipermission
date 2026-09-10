package backups

import (
	"context"
	"errors"
	"testing"
)

func TestOperationLimiterHonorsCancellationAndIdempotentRelease(t *testing.T) {
	limiter := &OperationLimiter{}
	releaseFirst, err := limiter.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire first operation: %v", err)
	}
	releaseSecond, err := limiter.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire second operation: %v", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := limiter.Acquire(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("third operation error = %v, want context.Canceled", err)
	}

	releaseFirst()
	releaseAfterSlot, err := limiter.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire operation after release: %v", err)
	}
	releaseAfterSlot()
	releaseSecond()
	releaseFirst()
	releaseSecond()
}
