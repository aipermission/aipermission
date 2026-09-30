package keycleanup

import (
	"errors"
	"testing"
)

func TestTargetHistoryFencesUnresolvedRetiredGroupsWithoutConflatingKeys(t *testing.T) {
	store := newMemoryStore()
	journal := New(store)
	original := beginTest(t, journal, testIdentity(t))
	other := testIdentity(t)
	other.KeyDigest, other.Profiles[0].KeyID = testDigest(t, "different-key"), 9
	replacement := beginTest(t, journal, other)
	if _, err := journal.Confirm(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	if err := journal.RequireTargetHistoryResolved(t.Context(), original.Record.Identity.TargetID); !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("completed replacement hid retired uncertainty: %v", err)
	}
	if _, err := journal.Confirm(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	if err := journal.RequireTargetHistoryResolved(t.Context(), original.Record.Identity.TargetID); err != nil {
		t.Fatalf("completed groups require another authentication: %v", err)
	}
	if store.secretReads != 0 {
		t.Fatal("history observation read secrets")
	}
}

func TestTargetHistoryRetainsOriginalAndAttestedTargetScopes(t *testing.T) {
	journal := New(newMemoryStore())
	identity := testIdentity(t)
	entry := beginTest(t, journal, identity)
	alias := testIdentity(t)
	alias.TargetID, alias.Profiles[0].ID = 10, 20
	if _, err := journal.Attest(t.Context(), entry, decisionForTest(t, entry, alias, "Externally verified historical and current key absence")); err != nil {
		t.Fatal(err)
	}
	for _, targetID := range []int64{identity.TargetID, alias.TargetID, 99} {
		entries, err := journal.ListForTarget(t.Context(), targetID)
		want := 1
		if targetID == 99 {
			want = 0
		}
		if err != nil || len(entries) != want {
			t.Fatalf("target scope %d lost or leaked history: %#v %v", targetID, entries, err)
		}
		if err := journal.RequireTargetHistoryResolved(t.Context(), targetID); err != nil {
			t.Fatalf("attested history remained unresolved: %v", err)
		}
	}
}

func TestTargetHistoryRejectsInvalidIdentityAndUnobservableEvidence(t *testing.T) {
	for _, targetID := range []int64{0, -1} {
		journal := New(newMemoryStore())
		if _, err := journal.ListForTarget(t.Context(), targetID); err == nil {
			t.Fatal("invalid target listed evidence")
		}
		if err := journal.RequireTargetHistoryResolved(t.Context(), targetID); err == nil {
			t.Fatal("invalid target passed history fence")
		}
	}
	store := newMemoryStore()
	broken := New(store)
	store.listErr = errors.New("journal list refused")
	for _, journal := range []*Journal{nil, New(nil), broken} {
		if err := journal.RequireTargetHistoryResolved(t.Context(), 1); err == nil {
			t.Fatal("unobservable history authorized cleanup")
		}
	}
	store.listErr = nil
	entry := beginTest(t, broken, testIdentity(t))
	row := store.rows[entry.ResourceID]
	row.PublicData = "{}"
	store.rows[entry.ResourceID] = row
	if err := broken.RequireTargetHistoryResolved(t.Context(), 99); err == nil {
		t.Fatal("malformed journal was ignored as unrelated")
	}
}

func TestTargetHistoryDoesNotBlockIndependentTargets(t *testing.T) {
	journal := New(newMemoryStore())
	beginTest(t, journal, testIdentity(t))
	if err := journal.RequireTargetHistoryResolved(t.Context(), 99); err != nil {
		t.Fatalf("independent target was blocked: %v", err)
	}
}
