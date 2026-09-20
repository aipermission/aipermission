package operationcoord

import (
	"context"
	"errors"
	"testing"
)

func TestCoordinatorScopesCancellationAndCleanup(t *testing.T) {
	var coordinator Coordinator[string]
	releaseFirst, err := coordinator.Acquire(t.Context(), "provider-a")
	if err != nil {
		t.Fatal(err)
	}
	releaseOther, err := coordinator.Acquire(t.Context(), "provider-b")
	if err != nil {
		t.Fatalf("different key was blocked: %v", err)
	}
	releaseOther()

	waitContext, cancel := context.WithCancel(t.Context())
	cancel()
	if release, err := coordinator.Acquire(waitContext, "provider-a"); !errors.Is(err, context.Canceled) || release != nil {
		t.Fatalf("blocked acquire = release:%v err:%v", release != nil, err)
	}
	releaseFirst()
	releaseFirst()

	releaseAgain, err := coordinator.Acquire(t.Context(), "provider-a")
	if err != nil {
		t.Fatalf("released key remained blocked: %v", err)
	}
	releaseAgain()

	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if len(coordinator.entries) != 0 {
		t.Fatalf("coordinator entries leaked: %d", len(coordinator.entries))
	}
}

func TestCoordinatorRejectsCanceledContextWhenTokenIsAvailable(t *testing.T) {
	var coordinator Coordinator[string]
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range 100 {
		release, err := coordinator.Acquire(ctx, "provider-a")
		if !errors.Is(err, context.Canceled) || release != nil {
			t.Fatalf("canceled acquire = release:%v err:%v", release != nil, err)
		}
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if len(coordinator.entries) != 0 {
		t.Fatalf("coordinator entries leaked: %d", len(coordinator.entries))
	}
}
