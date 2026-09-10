package vaultsessions

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestDeliveryCoordinatorRejectsExpiredContextBeforeAcquiringGate(t *testing.T) {
	coordinator := &DeliveryCoordinator{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for range 100 {
		if release, err := coordinator.AcquireDelivery(ctx); !errors.Is(err, context.Canceled) || release != nil {
			t.Fatalf("expired context acquired Vault delivery gate: release=%v err=%v", release != nil, err)
		}
	}

	release, err := coordinator.AcquireDelivery(context.Background())
	if err != nil {
		t.Fatalf("live context could not acquire Vault delivery gate: %v", err)
	}
	release()
}

func TestDeliveryCoordinatorAllowsConcurrentDeliveriesAndFencesMutation(t *testing.T) {
	coordinator := &DeliveryCoordinator{}
	first, err := coordinator.AcquireDelivery(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.AcquireDelivery(t.Context())
	if err != nil {
		t.Fatalf("second delivery was serialized: %v", err)
	}

	exclusiveAcquired := make(chan func(), 1)
	go func() {
		release, acquireErr := coordinator.AcquireExclusive(t.Context())
		if acquireErr == nil {
			exclusiveAcquired <- release
		}
	}()
	waitForDeliveryCoordinatorState(t, coordinator, func(readers int, writer bool, waitingWriters int) bool {
		return readers == 2 && !writer && waitingWriters == 1
	})

	thirdDelivery := make(chan func(), 1)
	go func() {
		release, acquireErr := coordinator.AcquireDelivery(t.Context())
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

func TestDeliveryCoordinatorCanceledWriterUnblocksDeliveries(t *testing.T) {
	coordinator := &DeliveryCoordinator{}
	activeRelease, err := coordinator.AcquireDelivery(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	writerContext, cancelWriter := context.WithCancel(t.Context())
	writerDone := make(chan error, 1)
	go func() {
		_, acquireErr := coordinator.AcquireExclusive(writerContext)
		writerDone <- acquireErr
	}()
	waitForDeliveryCoordinatorState(t, coordinator, func(readers int, writer bool, waitingWriters int) bool {
		return readers == 1 && !writer && waitingWriters == 1
	})

	cancelWriter()
	if err := <-writerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled writer error = %v", err)
	}
	nextRelease, err := coordinator.AcquireDelivery(t.Context())
	if err != nil {
		t.Fatalf("delivery remained blocked after writer cancellation: %v", err)
	}
	nextRelease()
	activeRelease()
}

func waitForDeliveryCoordinatorState(
	t *testing.T,
	coordinator *DeliveryCoordinator,
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
