package security

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

func TestNewOwnsSecurityRuntimeState(t *testing.T) {
	state := New(nil)
	if state.PolicyService() == nil || state.RuntimeControlState() == nil || state.VaultLeaseStore() == nil || state.VaultDeliveryCoordinator() == nil {
		t.Fatal("security runtime state is incomplete")
	}
	var nilState *State
	if nilState.PolicyService() != nil || nilState.RuntimeControlState() != nil || nilState.VaultLeaseStore() != nil || nilState.VaultDeliveryCoordinator() != nil {
		t.Fatal("nil security state exposed capabilities")
	}
}

func TestPendingVaultFinalizationBlocksSecretDelivery(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "security.aipdb"), "SecurityStatePassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := vaultfinalization.NewStore(database)
	id, err := store.Queue(t.Context(), vaultfinalization.Intent{Kind: "mutation", ItemID: 2})
	if err != nil {
		t.Fatal(err)
	}
	state := New(database)
	if _, err := state.VaultDeliveryCoordinator().AcquireDelivery(t.Context()); !errors.Is(err, vaultfinalization.ErrBlocked) {
		t.Fatalf("pending delivery = %v", err)
	}
	if err := store.Complete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	release, err := state.VaultDeliveryCoordinator().AcquireDelivery(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
	lifecycle := connectortargets.NewLifecycleFinalizationStore(database)
	if err := lifecycle.Queue(t.Context(), connectortargets.LifecycleFinalizationChange{TargetID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.VaultDeliveryCoordinator().AcquireDelivery(t.Context()); !errors.Is(err, connectortargets.ErrLifecycleFinalizationPending) {
		t.Fatalf("connector finalization did not block delivery: %v", err)
	}
	pending, err := lifecycle.Pending(t.Context())
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending finalization=%#v error=%v", pending, err)
	}
	if err := lifecycle.MarkVaultComplete(t.Context(), pending[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.MarkRequestsComplete(t.Context(), pending[0].ID); err != nil {
		t.Fatal(err)
	}
	release, err = state.VaultDeliveryCoordinator().AcquireDelivery(t.Context())
	if err != nil {
		t.Fatalf("completed connector finalization retained delivery fence: %v", err)
	}
	release()
}
