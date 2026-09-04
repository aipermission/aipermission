package api

import (
	"context"
	"errors"
	"testing"
)

func TestBackupOperationLimitHonorsCancellationAndRelease(t *testing.T) {
	server := &Server{}
	handlers := backupHandlers{server}

	releaseFirst, err := handlers.acquireBackupOperation(t.Context())
	if err != nil {
		t.Fatalf("acquire first backup operation: %v", err)
	}
	releaseSecond, err := handlers.acquireBackupOperation(t.Context())
	if err != nil {
		t.Fatalf("acquire second backup operation: %v", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := handlers.acquireBackupOperation(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("third backup operation error = %v, want context.Canceled", err)
	}

	releaseFirst()
	releaseAfterSlot, err := handlers.acquireBackupOperation(t.Context())
	if err != nil {
		t.Fatalf("acquire backup operation after release: %v", err)
	}
	releaseAfterSlot()
	releaseSecond()

	// Release functions are intentionally idempotent for deferred cleanup paths.
	releaseFirst()
	releaseSecond()
}
