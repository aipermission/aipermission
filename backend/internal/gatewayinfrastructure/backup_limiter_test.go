package gatewayinfrastructure

import (
	"context"
	"errors"
	"testing"
)

func TestBackupOperationLimiterHonorsCancellationAndIdempotentRelease(t *testing.T) {
	limiter := &backupOperationLimiter{}
	first, err := limiter.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := limiter.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := limiter.acquire(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire error = %v, want context canceled", err)
	}

	first()
	first()
	third, err := limiter.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	third()
	second()
}
