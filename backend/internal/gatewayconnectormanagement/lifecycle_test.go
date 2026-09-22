package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type lifecycleFinalizationMemory struct {
	items []connectortargets.PendingLifecycleFinalization
}

func finalizationMemory(change TargetLifecycleChange) *lifecycleFinalizationMemory {
	return &lifecycleFinalizationMemory{items: []connectortargets.PendingLifecycleFinalization{{
		ID: 1,
		Change: connectortargets.LifecycleFinalizationChange{
			TargetID: change.TargetID, ProfileID: change.ProfileID,
			StaleReason: change.StaleReason, UserMessage: change.UserMessage,
			IncludeRunning: change.IncludeRunning,
		},
		VaultPending: true, RequestsPending: true,
	}}}
}

func (memory *lifecycleFinalizationMemory) Pending(context.Context) ([]connectortargets.PendingLifecycleFinalization, error) {
	return append([]connectortargets.PendingLifecycleFinalization(nil), memory.items...), nil
}

func (memory *lifecycleFinalizationMemory) MarkVaultComplete(_ context.Context, id int64) error {
	for index := range memory.items {
		if memory.items[index].ID == id {
			memory.items[index].VaultPending = false
			memory.removeComplete(index)
			return nil
		}
	}
	return errors.New("finalization not found")
}

func (memory *lifecycleFinalizationMemory) MarkRequestsComplete(_ context.Context, id int64) error {
	for index := range memory.items {
		if memory.items[index].ID == id {
			memory.items[index].RequestsPending = false
			memory.removeComplete(index)
			return nil
		}
	}
	return errors.New("finalization not found")
}

func (*lifecycleFinalizationMemory) RecordAttempt(context.Context, int64) error { return nil }

func (memory *lifecycleFinalizationMemory) removeComplete(index int) {
	if !memory.items[index].VaultPending && !memory.items[index].RequestsPending {
		memory.items = append(memory.items[:index], memory.items[index+1:]...)
	}
}

func TestLifecycleDeleteTargetOwnsAuditPayloadWithoutMutatingCaller(t *testing.T) {
	original := map[string]any{"source": "test"}
	var actor, action string
	var payload map[string]any
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(_ context.Context, gotActor, gotAction string, build func() any, _ func(*sql.Tx) error) error {
			actor, action = gotActor, gotAction
			payload = build().(map[string]any)
			return nil
		},
		Redact:          func(_ context.Context, value string) string { return value },
		InvalidateVault: func(context.Context, int64, int64, string) error { return nil },
		Finalizations:   &lifecycleFinalizationMemory{},
	})
	target := Target{ID: 9, ConnectorKind: "fixture", Name: "primary"}

	if err := service.DeleteTarget(t.Context(), target, original); err != nil {
		t.Fatal(err)
	}
	if actor != "user" || action != "connector.target.deleted" {
		t.Fatalf("audit identity = %q %q", actor, action)
	}
	want := map[string]any{"source": "test", "target_id": int64(9), "connector_kind": "fixture", "name": "primary"}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = %#v, want %#v", payload, want)
	}
	if !reflect.DeepEqual(original, map[string]any{"source": "test"}) {
		t.Fatalf("caller payload mutated: %#v", original)
	}
}

func TestLifecycleCredentialChangeInvalidatesVaultBeforeRequests(t *testing.T) {
	steps := []string{}
	change := TargetLifecycleChange{
		TargetID: 4, ProfileID: 7, StaleReason: "vault stale", UserMessage: "request stale",
	}
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(_ context.Context, _, _ string, _ func() any, _ func(*sql.Tx) error) error {
			steps = append(steps, "requests")
			return nil
		},
		Redact: func(_ context.Context, value string) string { return value },
		InvalidateVault: func(_ context.Context, targetID, profileID int64, reason string) error {
			if targetID != 4 || profileID != 7 || reason != "vault stale" {
				t.Fatalf("vault invalidation = %d %d %q", targetID, profileID, reason)
			}
			steps = append(steps, "vault")
			return nil
		},
		Finalizations: finalizationMemory(change),
	})

	err := service.AfterCredentialChange(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(steps, []string{"vault", "requests"}) {
		t.Fatalf("lifecycle order = %#v", steps)
	}
}

func TestFinalizeDeletedTargetSurvivesRequestCancellation(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	observed := 0
	deadlines := []time.Time{}
	assertFinalizationContext := func(ctx context.Context) {
		t.Helper()
		if err := ctx.Err(); err != nil {
			t.Fatalf("finalization inherited request cancellation: %v", err)
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("finalization has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > deletedTargetFinalizationTimeout {
			t.Fatalf("finalization deadline remaining = %v", remaining)
		}
		deadlines = append(deadlines, deadline)
		observed++
	}
	change := TargetLifecycleChange{
		TargetID:    7,
		StaleReason: "connector target was deleted; send a fresh Vault request",
		UserMessage: "stale", IncludeRunning: true,
	}
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(ctx context.Context, _, _ string, _ func() any, _ func(*sql.Tx) error) error {
			assertFinalizationContext(ctx)
			return nil
		},
		Finalizations: finalizationMemory(change),
		Redact:        func(_ context.Context, value string) string { return value },
		InvalidateVault: func(ctx context.Context, _, _ int64, _ string) error {
			assertFinalizationContext(ctx)
			return nil
		},
	})

	if _, err := service.FinalizeDeletedTarget(requestCtx, Target{ID: 7}, "stale"); err != nil {
		t.Fatal(err)
	}
	if observed != 2 {
		t.Fatalf("finalization calls = %d, want 2", observed)
	}
	if len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) {
		t.Fatalf("finalization renewed its deadline between components: %v", deadlines)
	}
}

func TestFinalizeDeletedTargetAttemptsBothInvalidations(t *testing.T) {
	vaultErr := errors.New("vault invalidation timed out")
	requestErr := errors.New("request invalidation failed")
	requestInvalidationCalled := false
	change := TargetLifecycleChange{
		TargetID:    7,
		StaleReason: "connector target was deleted; send a fresh Vault request",
		UserMessage: "stale", IncludeRunning: true,
	}
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(ctx context.Context, _, _ string, _ func() any, _ func(*sql.Tx) error) error {
			requestInvalidationCalled = true
			if err := ctx.Err(); err != nil {
				t.Fatalf("request invalidation reused failed Vault context: %v", err)
			}
			return requestErr
		},
		Redact: func(_ context.Context, value string) string { return value },
		InvalidateVault: func(context.Context, int64, int64, string) error {
			return vaultErr
		},
		Finalizations: finalizationMemory(change),
	})

	_, err := service.FinalizeDeletedTarget(t.Context(), Target{ID: 7}, "stale")
	if !requestInvalidationCalled {
		t.Fatal("request invalidation was skipped after Vault invalidation failed")
	}
	if !errors.Is(err, vaultErr) || !errors.Is(err, requestErr) {
		t.Fatalf("finalization error = %v, want both invalidation errors", err)
	}
}

func TestLifecycleCredentialChangeAttemptsBothInvalidations(t *testing.T) {
	vaultErr := errors.New("vault unavailable")
	requestErr := errors.New("request invalidation failed")
	mutated := false
	change := TargetLifecycleChange{TargetID: 2}
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(ctx context.Context, _ string, _ string, _ func() any, _ func(*sql.Tx) error) error {
			if err := ctx.Err(); err != nil {
				t.Fatalf("request invalidation reused failed Vault context: %v", err)
			}
			mutated = true
			return requestErr
		},
		Redact:          func(_ context.Context, value string) string { return value },
		InvalidateVault: func(context.Context, int64, int64, string) error { return vaultErr },
		Finalizations:   finalizationMemory(change),
	})

	err := service.AfterCredentialChange(t.Context(), change)
	if !errors.Is(err, vaultErr) || !errors.Is(err, requestErr) {
		t.Fatalf("error = %v, want both invalidation errors", err)
	}
	if !mutated {
		t.Fatal("request invalidation was skipped after Vault invalidation failed")
	}
}

func TestLifecycleRecoveryRetriesOnlyPendingComponent(t *testing.T) {
	change := TargetLifecycleChange{TargetID: 5, ProfileID: 9}
	memory := finalizationMemory(change)
	vaultAttempts := 0
	requestAttempts := 0
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(context.Context, string, string, func() any, func(*sql.Tx) error) error {
			requestAttempts++
			return nil
		},
		Redact: func(_ context.Context, value string) string { return value },
		InvalidateVault: func(context.Context, int64, int64, string) error {
			vaultAttempts++
			if vaultAttempts == 1 {
				return errors.New("temporary Vault cleanup failure")
			}
			return nil
		},
		Finalizations: memory,
	})

	if err := service.AfterCredentialChange(t.Context(), change); !errors.Is(err, connectortargets.ErrLifecycleFinalizationPending) {
		t.Fatalf("first finalization error = %v", err)
	}
	if len(memory.items) != 1 || !memory.items[0].VaultPending || memory.items[0].RequestsPending {
		t.Fatalf("independent component state = %#v", memory.items)
	}
	if err := service.RecoverPending(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(memory.items) != 0 || vaultAttempts != 2 || requestAttempts != 1 {
		t.Fatalf("recovery state items=%#v vault=%d requests=%d", memory.items, vaultAttempts, requestAttempts)
	}
}

func TestLifecycleFailureBlocksDeliveryUntilDurableRecoveryCompletes(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "lifecycle-recovery.aipdb"), "LifecycleRecoveryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	change := TargetLifecycleChange{
		TargetID: 12, ProfileID: 4, StaleReason: "credential changed", UserMessage: "request stale",
	}
	finalizations := connectortargets.NewLifecycleFinalizationStore(database)
	if err := finalizations.Queue(t.Context(), connectortargets.LifecycleFinalizationChange{
		TargetID: change.TargetID, ProfileID: change.ProfileID,
		StaleReason: change.StaleReason, UserMessage: change.UserMessage,
	}); err != nil {
		t.Fatal(err)
	}
	vaultAttempts := 0
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(context.Context, string, string, func() any, func(*sql.Tx) error) error {
			return nil
		},
		Redact: func(_ context.Context, value string) string { return value },
		InvalidateVault: func(context.Context, int64, int64, string) error {
			vaultAttempts++
			if vaultAttempts == 1 {
				return errors.New("temporary Vault cleanup failure")
			}
			return nil
		},
		Finalizations: finalizations,
	})
	if err := service.AfterCredentialChange(t.Context(), change); !errors.Is(err, connectortargets.ErrLifecycleFinalizationPending) {
		t.Fatalf("initial finalization error = %v", err)
	}
	coordinator := &vaultsessions.DeliveryCoordinator{}
	coordinator.SetDeliveryGuard(finalizations.RequireReady)
	if release, err := coordinator.AcquireDelivery(t.Context()); !errors.Is(err, connectortargets.ErrLifecycleFinalizationPending) {
		if release != nil {
			release()
		}
		t.Fatalf("delivery guard error = %v", err)
	}
	if err := service.RecoverPending(t.Context()); err != nil {
		t.Fatal(err)
	}
	release, err := coordinator.AcquireDelivery(t.Context())
	if err != nil {
		t.Fatalf("delivery remained blocked after recovery: %v", err)
	}
	release()
	if vaultAttempts != 2 {
		t.Fatalf("Vault finalization attempts = %d, want 2", vaultAttempts)
	}
}

func TestLifecycleRejectsIncompleteDependencies(t *testing.T) {
	err := NewLifecycleService(LifecycleServiceDependencies{}).DeleteTarget(
		t.Context(), Target{ID: 1}, nil,
	)
	if !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("error = %v, want %v", err, ErrLifecycleUnavailable)
	}
}
