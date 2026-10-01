package rolejournal

import (
	"context"
	"errors"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type transitionCase struct {
	name    string
	prepare func(*testing.T, *Journal) Entry
	run     func(context.Context, *Journal, Entry) (Entry, error)
	status  Status
}

func journalTransitions() []transitionCase {
	return []transitionCase{
		{"bind role", beginTest, func(ctx context.Context, j *Journal, e Entry) (Entry, error) { return j.BindRole(ctx, e, 42) }, ProvisionIntent},
		{"confirm provisioning", boundTest, func(ctx context.Context, j *Journal, e Entry) (Entry, error) { return j.ConfirmProvision(ctx, e) }, Provisioned},
		{"confirm rollback", boundTest, func(ctx context.Context, j *Journal, e Entry) (Entry, error) { return j.ConfirmRollback(ctx, e) }, RolledBack},
		{"begin cleanup", provisionTest, func(ctx context.Context, j *Journal, e Entry) (Entry, error) {
			entry, dispatch, err := j.BeginCleanup(ctx, e)
			if dispatch {
				return Entry{}, errors.New("failed journal write authorized remote dispatch")
			}
			return entry, err
		}, CleanupIntent},
		{"confirm cleanup", cleanupTest, func(ctx context.Context, j *Journal, e Entry) (Entry, error) { return j.ConfirmCleanup(ctx, e) }, Cleaned},
		{"confirm cleanup rollback", cleanupTest, func(ctx context.Context, j *Journal, e Entry) (Entry, error) { return j.ConfirmCleanupRollback(ctx, e) }, Provisioned},
		{"confirm cleanup not applied", cleanupTest, func(ctx context.Context, j *Journal, e Entry) (Entry, error) {
			return j.ConfirmCleanupNotApplied(ctx, e)
		}, Provisioned},
	}
}

func assertPersistedTransition(t *testing.T, before, after Entry, status Status) {
	t.Helper()
	if after.Record.Status != status || after.Record.Generation == before.Record.Generation ||
		after.ResourceID != before.ResourceID || after.Record.Intent != before.Record.Intent ||
		after.Record.Version != before.Record.Version || after.Record.RoleOID != 42 {
		t.Fatalf("durable decision or exact ownership identity lost: before %#v, after %#v", before, after)
	}
}

func TestEveryTransitionPreservesUncertaintyOnLostAcknowledgement(t *testing.T) {
	for _, step := range journalTransitions() {
		for _, after := range []bool{false, true} {
			t.Run(step.name+map[bool]string{false: "/before write", true: "/after write"}[after], func(t *testing.T) {
				store := newMemoryStore()
				journal := New(store)
				entry := step.prepare(t, journal)
				fault := errors.New("lost storage acknowledgement")
				store.updateErr, store.after = fault, after
				if got, err := step.run(t.Context(), journal, entry); !errors.Is(err, fault) || got.ResourceID != 0 {
					t.Fatalf("uncertain write accepted: %#v %v", got, err)
				}
				store.updateErr = nil
				fresh, err := New(store).Get(t.Context(), entry.ResourceID)
				if err != nil {
					t.Fatal(err)
				}
				if after {
					assertPersistedTransition(t, entry, fresh, step.status)
					if _, err := step.run(t.Context(), journal, entry); !errors.Is(err, ErrStaleGeneration) {
						t.Fatalf("stale decision replayed: %v", err)
					}
				} else if fresh != entry {
					t.Fatal("failed write changed durable state")
				}
			})
		}
	}
}

func TestCanceledJournalDoesNotAuthorizeMutation(t *testing.T) {
	store := newMemoryStore()
	journal := New(store)
	entry := provisionTest(t, journal)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	beforeCreate, beforeUpdate := store.creates, store.updates
	if _, err := journal.BeginProvision(ctx, testAnchor(), "another"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled provisioning accepted: %v", err)
	}
	if _, dispatch, err := journal.BeginCleanup(ctx, entry); dispatch || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cleanup authorized dispatch: %t %v", dispatch, err)
	}
	if _, err := store.Create(ctx, resourcecontract.CreateCredentialResourceInput{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := store.Update(ctx, entry.ResourceID, resourcecontract.UpdateCredentialResourceInput{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if store.creates != beforeCreate || store.updates != beforeUpdate {
		t.Fatal("canceled operations mutated store")
	}
}

func TestTransitionRequiresFreshFullRecordAndLegalState(t *testing.T) {
	store := newMemoryStore()
	journal := New(store)
	entry := provisionTest(t, journal)
	if err := journal.ValidateCurrent(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Entry){
		func(e *Entry) { e.Record.Generation = "ffffffffffffffffffffffffffffffff" },
		func(e *Entry) { e.Record.RoleOID++ },
		func(e *Entry) { e.Record.Intent.Anchor.AdminProfileID++ },
		func(e *Entry) { e.Record.Intent.Anchor.ClusterID = "123" },
		func(e *Entry) { e.Record.Intent.RoleName = "another" },
		func(e *Entry) { e.Record.Status = Cleaned },
	} {
		stale := entry
		mutate(&stale)
		before := store.updates
		if err := journal.ValidateCurrent(t.Context(), stale); !errors.Is(err, ErrStaleGeneration) || store.updates != before {
			t.Fatalf("stale record passed pre-connection validation: %v", err)
		}
		if _, dispatch, err := journal.BeginCleanup(t.Context(), stale); dispatch || !errors.Is(err, ErrStaleGeneration) || store.updates != before {
			t.Fatalf("stale record authorized mutation: %t %v", dispatch, err)
		}
	}
	if _, err := journal.ConfirmCleanup(t.Context(), entry); !errors.Is(err, ErrReconciliationRequired) {
		t.Fatal("cleanup confirmed without intent")
	}
	if _, err := journal.ConfirmRollback(t.Context(), entry); !errors.Is(err, ErrReconciliationRequired) {
		t.Fatal("provisioned role marked rolled back")
	}
	if _, err := journal.ConfirmProvision(t.Context(), entry); !errors.Is(err, ErrReconciliationRequired) {
		t.Fatal("provisioned role confirmed twice")
	}
	invalid := entry
	invalid.Record.Version = 99
	if _, _, err := journal.BeginCleanup(t.Context(), invalid); err == nil {
		t.Fatal("invalid record accepted")
	}
	store.getErr = errors.New("read unavailable")
	if _, _, err := journal.BeginCleanup(t.Context(), entry); err == nil {
		t.Fatal("failed fresh read accepted")
	}
}

func TestBindingRejectsZeroOrSuccessorOID(t *testing.T) {
	store := newMemoryStore()
	journal := New(store)
	entry := beginTest(t, journal)
	for _, oid := range []uint32{0, entry.Record.Intent.Anchor.SuccessorOID} {
		if _, err := journal.BindRole(t.Context(), entry, oid); err == nil {
			t.Fatalf("unsafe role OID %d accepted", oid)
		}
	}
}

func TestTransitionReadbackCannotSwapRecord(t *testing.T) {
	for _, mode := range []string{"another ID", "changed generation", "different durable generation", "read unavailable"} {
		t.Run(mode, func(t *testing.T) {
			store := newMemoryStore()
			journal := New(store)
			entry := provisionTest(t, journal)
			store.transform = func(row resourcecontract.CredentialResource) resourcecontract.CredentialResource {
				if mode == "another ID" {
					row.ID++
					return row
				}
				if mode == "read unavailable" {
					store.getErr = errors.New("read unavailable")
					return row
				}
				parsed, err := parseResource(row)
				if err != nil {
					t.Fatal(err)
				}
				changed := parsed.Record
				changed.Generation = "ffffffffffffffffffffffffffffffff"
				if mode == "different durable generation" {
					store.rows[row.ID] = encodeTestRecord(t, row, changed)
				} else {
					row = encodeTestRecord(t, row, changed)
				}
				return row
			}
			if _, dispatch, err := journal.BeginCleanup(t.Context(), entry); err == nil || dispatch {
				t.Fatalf("unconfirmed cleanup authorized dispatch: %t %v", dispatch, err)
			}
			store.transform, store.getErr = nil, nil
			fresh, err := journal.Get(t.Context(), entry.ResourceID)
			if err != nil || fresh.Record.Status != CleanupIntent {
				t.Fatalf("uncertainty lost: %#v %v", fresh, err)
			}
			if _, dispatch, err := journal.BeginCleanup(t.Context(), fresh); dispatch || !errors.Is(err, ErrReconciliationRequired) {
				t.Fatal("uncertain operation redispatched after readback failure")
			}
		})
	}
}

func beginTest(t *testing.T, journal *Journal) Entry {
	t.Helper()
	entry, err := journal.BeginProvision(t.Context(), testAnchor(), " My Role ")
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func boundTest(t *testing.T, journal *Journal) Entry {
	t.Helper()
	entry, err := journal.BindRole(t.Context(), beginTest(t, journal), 42)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func cleanupTest(t *testing.T, journal *Journal) Entry {
	t.Helper()
	entry, dispatch, err := journal.BeginCleanup(t.Context(), provisionTest(t, journal))
	if err != nil || !dispatch {
		t.Fatalf("cleanup intent: %t %v", dispatch, err)
	}
	return entry
}
