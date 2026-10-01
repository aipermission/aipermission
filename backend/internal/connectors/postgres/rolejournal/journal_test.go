package rolejournal

import (
	"context"
	"errors"
	"reflect"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func TestDurableRoleLifecycle(t *testing.T) {
	store := newMemoryStore()
	journal := New(store)
	intent, err := journal.BeginProvision(t.Context(), testAnchor(), " My Role ")
	if err != nil || intent.Record.Status != ProvisionIntent || intent.Record.RoleOID != 0 {
		t.Fatalf("intent not persisted: %#v %v", intent, err)
	}
	if _, err := journal.ConfirmProvision(t.Context(), intent); err == nil {
		t.Fatal("unbound role confirmed")
	}
	bound, err := journal.BindRole(t.Context(), intent, 42)
	if err != nil || bound.Record.Generation == intent.Record.Generation {
		t.Fatalf("role binding failed: %#v %v", bound, err)
	}
	if _, err := journal.BindRole(t.Context(), bound, 43); err == nil {
		t.Fatal("role OID rebound")
	}
	ready, err := journal.ConfirmProvision(t.Context(), bound)
	if err != nil {
		t.Fatal(err)
	}
	cleanup, dispatch, err := journal.BeginCleanup(t.Context(), ready)
	if err != nil || !dispatch || cleanup.Record.Status != CleanupIntent {
		t.Fatalf("cleanup intent missing: %#v %t %v", cleanup, dispatch, err)
	}
	if _, dispatch, err := journal.BeginCleanup(t.Context(), cleanup); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("uncertain cleanup redispatched: %t %v", dispatch, err)
	}
	finished, err := journal.ConfirmCleanup(t.Context(), cleanup)
	if err != nil || finished.Record.Status != Cleaned {
		t.Fatalf("cleanup confirmation failed: %#v %v", finished, err)
	}
	// Reconstruct the service: the decision lives in its scoped store, not in RAM.
	journal = New(store)
	if got, dispatch, err := journal.BeginCleanup(t.Context(), finished); err != nil || dispatch || got != finished {
		t.Fatalf("confirmed cleanup not resumed: %#v %t %v", got, dispatch, err)
	}
	if finished.Record.Intent != intent.Record.Intent || finished.Record.RoleOID != 42 || store.reads != 0 {
		t.Fatal("ownership evidence mutated or secret read")
	}
	if _, err := journal.BeginProvision(t.Context(), testAnchor(), " My Role "); err != nil {
		t.Fatalf("terminal cleanup did not release role-name fence: %v", err)
	}
}

func TestProvisionFenceIncludesAllNonterminalStates(t *testing.T) {
	for _, status := range []Status{ProvisionIntent, Provisioned, CleanupIntent} {
		t.Run(string(status), func(t *testing.T) {
			for _, drift := range []string{"target endpoint", "target alias", "admin", "database"} {
				t.Run(drift, func(t *testing.T) {
					store := newMemoryStore()
					journal := New(store)
					entry := provisionTest(t, journal)
					record := entry.Record
					record.Status = status
					store.rows[entry.ResourceID] = encodeTestRecord(t, store.rows[entry.ResourceID], record)
					anchor := testAnchor()
					switch drift {
					case "target endpoint":
						anchor.ClusterID = "123"
					case "target alias":
						anchor.TargetID++
					case "admin":
						anchor.AdminProfileID++
						anchor.SuccessorOID++
					case "database":
						anchor.DatabaseOID++
						anchor.DatabaseName = "another"
					}
					if _, err := journal.BeginProvision(t.Context(), anchor, " My Role "); !errors.Is(err, ErrReconciliationRequired) || store.creates != 1 {
						t.Fatalf("unresolved predecessor bypassed: %v, creates %d", err, store.creates)
					}
				})
			}
		})
	}
}

func TestAcknowledgedRollbackReleasesProvisionFence(t *testing.T) {
	store := newMemoryStore()
	journal := New(store)
	entry, err := journal.BeginProvision(t.Context(), testAnchor(), "role")
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := journal.ConfirmRollback(t.Context(), entry)
	if err != nil || rolledBack.Record.Status != RolledBack {
		t.Fatalf("rollback: %#v %v", rolledBack, err)
	}
	if _, dispatch, err := journal.BeginCleanup(t.Context(), rolledBack); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatal("rolled-back provisioning authorized a destructive cleanup")
	}
	if _, err := journal.BeginProvision(t.Context(), testAnchor(), "role"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateUncertaintyDoesNotAuthorizeRemoteMutation(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before persistence", true: "lost acknowledgement"}[after], func(t *testing.T) {
			store := newMemoryStore()
			store.createErr, store.after = errors.New("storage failure"), after
			journal := New(store)
			if entry, err := journal.BeginProvision(t.Context(), testAnchor(), "role"); err == nil || entry.ResourceID != 0 {
				t.Fatalf("failed write authorized dispatch: %#v %v", entry, err)
			}
			store.createErr = nil
			_, err := journal.BeginProvision(t.Context(), testAnchor(), "role")
			if after && !errors.Is(err, ErrReconciliationRequired) {
				t.Fatalf("lost write escaped fence: %v", err)
			}
			if !after && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFreshPersistenceReadbackIsRequired(t *testing.T) {
	for _, mode := range []string{"invalid response", "valid but changed response", "missing readback", "different readback"} {
		t.Run(mode, func(t *testing.T) {
			store := newMemoryStore()
			switch mode {
			case "missing readback":
				store.getErr = errors.New("storage unavailable")
			case "invalid response":
				store.transform = func(row resourcecontract.CredentialResource) resourcecontract.CredentialResource {
					row.PublicData = "{}"
					return row
				}
			case "valid but changed response", "different readback":
				store.transform = func(row resourcecontract.CredentialResource) resourcecontract.CredentialResource {
					entry, err := parseResource(row)
					if err != nil {
						t.Fatal(err)
					}
					changed := entry.Record
					changed.Generation = "ffffffffffffffffffffffffffffffff"
					if mode == "different readback" {
						store.rows[row.ID] = encodeTestRecord(t, row, changed)
					} else {
						row = encodeTestRecord(t, row, changed)
					}
					return row
				}
			}
			journal := New(store)
			if _, err := journal.BeginProvision(t.Context(), testAnchor(), "role"); err == nil {
				t.Fatal("unconfirmed write accepted")
			}
			store.getErr, store.transform = nil, nil
			if _, err := journal.BeginProvision(t.Context(), testAnchor(), "role"); !errors.Is(err, ErrReconciliationRequired) {
				t.Fatal("unconfirmed record escaped fence")
			}
		})
	}
}

func TestJournalRejectsMissingAuthorityAndInvalidRecords(t *testing.T) {
	for _, journal := range []*Journal{nil, New(nil)} {
		if _, err := journal.List(t.Context()); err == nil {
			t.Fatal("missing store accepted")
		}
		if _, err := journal.Get(t.Context(), 1); err == nil {
			t.Fatal("missing store accepted")
		}
		if _, err := journal.BeginProvision(t.Context(), testAnchor(), "role"); err == nil {
			t.Fatal("missing store accepted")
		}
	}
	store := newMemoryStore()
	journal := New(store)
	if _, err := journal.Get(t.Context(), 0); err == nil {
		t.Fatal("missing ID accepted")
	}
	if _, err := journal.BeginProvision(t.Context(), testAnchor(), ""); err == nil || store.creates != 0 {
		t.Fatal("invalid intent persisted")
	}
	store.listErr = errors.New("list unavailable")
	if _, err := journal.List(t.Context()); err == nil {
		t.Fatal("list error ignored")
	}
	store.listErr = nil
	row := resourceTest(t)
	store.listed = []resourcecontract.CredentialResource{row, row}
	if _, err := journal.List(t.Context()); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	other := row
	other.ID++
	store.listed[1] = other
	if _, err := journal.List(t.Context()); err == nil {
		t.Fatal("duplicate operation accepted")
	}
	store.listed = []resourcecontract.CredentialResource{{ID: 1, PublicData: "{}"}}
	if _, err := journal.List(t.Context()); err == nil {
		t.Fatal("corrupt list entry accepted")
	}
	store.listed = nil
	store.rows[1] = row
	store.rows[2] = row
	if _, err := journal.Get(t.Context(), 2); err == nil {
		t.Fatal("foreign record ID accepted")
	}
	if got, err := journal.Get(context.Background(), 1); err != nil || !reflect.DeepEqual(got.Record.Intent.Anchor, testAnchor()) {
		t.Fatalf("valid record rejected: %#v %v", got, err)
	}
}
