package api

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestVaultDeliveryCoordinatorRejectsExpiredContextBeforeAcquiringGate(t *testing.T) {
	coordinator := &vaultDeliveryCoordinator{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for range 100 {
		if release, err := coordinator.acquireDelivery(ctx); !errors.Is(err, context.Canceled) || release != nil {
			t.Fatalf("expired context acquired Vault delivery gate: release=%v err=%v", release != nil, err)
		}
	}

	release, err := coordinator.acquireDelivery(context.Background())
	if err != nil {
		t.Fatalf("live context could not acquire Vault delivery gate: %v", err)
	}
	release()
}

func TestVaultDeliveryCoordinatorAllowsConcurrentDeliveriesAndFencesMutation(t *testing.T) {
	coordinator := &vaultDeliveryCoordinator{}
	first, err := coordinator.acquireDelivery(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.acquireDelivery(t.Context())
	if err != nil {
		t.Fatalf("second delivery was serialized: %v", err)
	}

	exclusiveAcquired := make(chan func(), 1)
	go func() {
		release, acquireErr := coordinator.acquireExclusive(t.Context())
		if acquireErr == nil {
			exclusiveAcquired <- release
		}
	}()
	waitForVaultCoordinatorState(t, coordinator, func(readers int, writer bool, waitingWriters int) bool {
		return readers == 2 && !writer && waitingWriters == 1
	})

	thirdDelivery := make(chan func(), 1)
	go func() {
		release, acquireErr := coordinator.acquireDelivery(t.Context())
		if acquireErr == nil {
			thirdDelivery <- release
		}
	}()
	select {
	case release := <-exclusiveAcquired:
		release()
		t.Fatal("exclusive mutation crossed active deliveries")
	default:
	}
	select {
	case release := <-thirdDelivery:
		release()
		t.Fatal("new delivery bypassed a waiting exclusive mutation")
	default:
	}
	first()
	second()
	exclusiveRelease := <-exclusiveAcquired
	select {
	case release := <-thirdDelivery:
		release()
		t.Fatal("new delivery crossed the active exclusive mutation")
	default:
	}
	exclusiveRelease()
	(<-thirdDelivery)()
}

func TestVaultDeliveryCoordinatorCanceledWriterUnblocksDeliveries(t *testing.T) {
	coordinator := &vaultDeliveryCoordinator{}
	activeRelease, err := coordinator.acquireDelivery(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	writerContext, cancelWriter := context.WithCancel(t.Context())
	writerDone := make(chan error, 1)
	go func() {
		_, acquireErr := coordinator.acquireExclusive(writerContext)
		writerDone <- acquireErr
	}()
	waitForVaultCoordinatorState(t, coordinator, func(readers int, writer bool, waitingWriters int) bool {
		return readers == 1 && !writer && waitingWriters == 1
	})

	cancelWriter()
	if err := <-writerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled writer error = %v", err)
	}
	nextRelease, err := coordinator.acquireDelivery(t.Context())
	if err != nil {
		t.Fatalf("delivery remained blocked after writer cancellation: %v", err)
	}
	nextRelease()
	activeRelease()
}

func waitForVaultCoordinatorState(
	t *testing.T,
	coordinator *vaultDeliveryCoordinator,
	matches func(readers int, writer bool, waitingWriters int) bool,
) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		coordinator.mu.Lock()
		matched := matches(coordinator.readers, coordinator.writer, coordinator.waitingWriters)
		coordinator.mu.Unlock()
		if matched {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("timed out waiting for Vault delivery coordinator state")
}
