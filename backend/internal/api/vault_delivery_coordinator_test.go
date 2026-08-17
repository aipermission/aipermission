package api

import (
	"context"
	"errors"
	"testing"
)

func TestVaultDeliveryCoordinatorRejectsExpiredContextBeforeAcquiringGate(t *testing.T) {
	coordinator := &vaultDeliveryCoordinator{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for range 100 {
		if release, err := coordinator.acquire(ctx); !errors.Is(err, context.Canceled) || release != nil {
			t.Fatalf("expired context acquired Vault delivery gate: release=%v err=%v", release != nil, err)
		}
	}

	release, err := coordinator.acquire(context.Background())
	if err != nil {
		t.Fatalf("live context could not acquire Vault delivery gate: %v", err)
	}
	release()
}
