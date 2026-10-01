package rolecatalog

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/jackc/pgx/v5"
)

// Observe the real journal's persisted readback without changing shared fixtures.
type reconciliationStore struct {
	*lifecycleStore
	afterGet func(context.Context, resourcecontract.CredentialResource) error
}

func (store *reconciliationStore) Get(ctx context.Context, id int64) (resourcecontract.CredentialResource, error) {
	row, err := store.lifecycleStore.Get(ctx, id)
	if err == nil && store.afterGet != nil {
		err = store.afterGet(ctx, row)
	}
	return row, err
}

func reconciliationFixture(t *testing.T, status rolejournal.Status, bound bool) (*rolejournal.Journal, *reconciliationStore, *lifecycleTx, *lifecycleConnection, rolejournal.Entry) {
	t.Helper()
	_, base, tx, connection := lifecycleFixture(t)
	store := &reconciliationStore{lifecycleStore: base}
	journal := rolejournal.New(store)
	entry, err := journal.BeginProvision(t.Context(), testRecord().Intent.Anchor, testRecord().Intent.RoleName)
	if err == nil && bound {
		entry, err = journal.BindRole(t.Context(), entry, 42)
	}
	if err == nil && status == rolejournal.RolledBack {
		entry, err = journal.ConfirmRollback(t.Context(), entry)
	} else if err == nil && status != rolejournal.ProvisionIntent {
		entry, err = journal.ConfirmProvision(t.Context(), entry)
		if err == nil && status != rolejournal.Provisioned {
			entry, _, err = journal.BeginCleanup(t.Context(), entry)
			if err == nil && status == rolejournal.Cleaned {
				entry, err = journal.ConfirmCleanup(t.Context(), entry)
			}
		}
	}
	if err != nil || entry.Record.Status != status {
		t.Fatalf("fixture status=%s entry=%#v err=%v", status, entry, err)
	}
	return journal, store, tx, connection, entry
}

func assertReconciliationReleased(t *testing.T, tx *lifecycleTx, connection *lifecycleConnection) {
	t.Helper()
	if !reflect.DeepEqual(tx.statements, []string{catalogLock}) || tx.commits != 0 {
		t.Fatalf("reconciliation executed more than its fence: statements=%q commits=%d", tx.statements, tx.commits)
	}
	if tx.rollbacks != 1 || connection.closes != 1 || tx.rollbackCanceled || connection.closeCanceled ||
		tx.rollbackDeadline <= 0 || tx.rollbackDeadline > 5*time.Second ||
		connection.closeDeadline <= 0 || connection.closeDeadline > 5*time.Second {
		t.Fatalf("cleanup not detached and bounded: rollbacks=%d closes=%d canceled=%t/%t deadlines=%v/%v",
			tx.rollbacks, connection.closes, tx.rollbackCanceled, connection.closeCanceled, tx.rollbackDeadline, connection.closeDeadline)
	}
}

func assertReconciliationUnknown(t *testing.T, err error, entry rolejournal.Entry) {
	t.Helper()
	details := connectors.ErrorDetails(err)
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" ||
		details["dispatch_stage"] != "role_reconciliation_confirmation" || details["retry_safe"] != false ||
		details["reconciliation_required"] != true || details["journal_resource_id"] != entry.Reference().ResourceID ||
		details["journal_generation"] != entry.Record.Generation || details["journal_status"] != string(entry.Record.Status) {
		t.Fatalf("unsafe confirmation error=%v details=%#v", err, details)
	}
}

func TestReconcileConfirmsBoundIntentUnderFenceThroughReadback(t *testing.T) {
	for _, status := range []rolejournal.Status{rolejournal.ProvisionIntent, rolejournal.CleanupIntent} {
		t.Run(string(status), func(t *testing.T) {
			journal, store, tx, connection, entry := reconciliationFixture(t, status, true)
			writes, readbacks, calls := 0, 0, 0
			assertHeld := func() {
				if tx.rollbacks != 0 || connection.closes != 0 || tx.commits != 0 ||
					!reflect.DeepEqual(tx.statements, []string{catalogLock}) ||
					!reflect.DeepEqual(tx.queries, []string{anchorQuery, roleQuery}) {
					t.Fatal("confirmation or readback did not retain the verified catalog fence")
				}
			}
			store.beforeUpdate = func(next rolejournal.Record) {
				writes++
				assertHeld()
				if next.Status != rolejournal.Provisioned || next.Generation == entry.Record.Generation {
					t.Fatalf("confirmation did not advance state/generation: %#v", next)
				}
			}
			store.afterGet = func(_ context.Context, _ resourcecontract.CredentialResource) error {
				if store.last.Status == rolejournal.Provisioned {
					readbacks++
					assertHeld()
				}
				return nil
			}
			got, err := Reconcile(t.Context(), journal, entry, entry.Record.Intent.Anchor,
				func(context.Context) (Connection, error) { calls++; return connection, nil })
			if err != nil || calls != 1 || writes != 1 || readbacks != 1 || got.Record.Generation == entry.Record.Generation {
				t.Fatalf("confirmation=%#v err=%v calls/writes/readbacks=%d/%d/%d", got, err, calls, writes, readbacks)
			}
			want := entry
			want.Record.Status, want.Record.Generation = rolejournal.Provisioned, got.Record.Generation
			if got != want || got.Record.Validate() != nil || got.Reference() != entry.Reference() {
				t.Fatalf("confirmation changed immutable identity: got=%#v want=%#v", got, want)
			}
			store.afterGet = nil
			fresh, err := journal.Get(t.Context(), entry.ResourceID)
			if err != nil || fresh != got {
				t.Fatalf("fresh confirmation=%#v err=%v", fresh, err)
			}
			assertReconciliationReleased(t, tx, connection)
		})
	}
}

func TestReconcileRejectsEntireStaleSnapshotBeforeDial(t *testing.T) {
	for _, field := range []string{"generation", "status", "role OID", "operation", "role name", "target", "profile", "digest", "cluster", "database OID", "database name", "successor OID", "successor name"} {
		t.Run(field, func(t *testing.T) {
			journal, store, tx, connection, entry := reconciliationFixture(t, rolejournal.ProvisionIntent, true)
			stale := entry
			switch field {
			case "generation":
				stale.Record.Generation = testRecord().Generation
			case "status":
				stale.Record.Status = rolejournal.CleanupIntent
			case "role OID":
				stale.Record.RoleOID++
			case "operation":
				stale.Record.Intent.OperationID = testRecord().Intent.OperationID
			case "role name":
				stale.Record.Intent.RoleName = "other"
			case "target":
				stale.Record.Intent.Anchor.TargetID++
			case "profile":
				stale.Record.Intent.Anchor.AdminProfileID++
			case "digest":
				stale.Record.Intent.Anchor.ContextDigest = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
			case "cluster":
				stale.Record.Intent.Anchor.ClusterID = "1"
			case "database OID":
				stale.Record.Intent.Anchor.DatabaseOID++
			case "database name":
				stale.Record.Intent.Anchor.DatabaseName = "other"
			case "successor OID":
				stale.Record.Intent.Anchor.SuccessorOID++
			case "successor name":
				stale.Record.Intent.Anchor.SuccessorName = "other"
			}
			calls := 0
			got, err := Reconcile(t.Context(), journal, stale, stale.Record.Intent.Anchor,
				func(context.Context) (Connection, error) { calls++; return connection, nil })
			if !errors.Is(err, rolejournal.ErrStaleGeneration) || got != (rolejournal.Entry{}) || calls != 0 || store.last != entry.Record ||
				len(tx.statements) != 0 || tx.rollbacks != 0 || connection.closes != 0 {
				t.Fatalf("stale snapshot dispatched: got=%#v err=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestReconcileRejectsIneligibleRequestsBeforeDial(t *testing.T) {
	for _, mode := range []string{"invalid authority", "target drift", "profile drift", "digest drift", "database drift", "successor drift", "nil context", "canceled", "expired", "nil journal", "journal read", "unbound", "provisioned", "cleaned", "rolled back"} {
		t.Run(mode, func(t *testing.T) {
			status, bound := rolejournal.ProvisionIntent, mode != "unbound"
			switch mode {
			case "provisioned":
				status = rolejournal.Provisioned
			case "cleaned":
				status = rolejournal.Cleaned
			case "rolled back":
				status = rolejournal.RolledBack
			}
			journal, store, tx, connection, entry := reconciliationFixture(t, status, bound)
			authority, ctx := entry.Record.Intent.Anchor, t.Context()
			var wantErr error
			switch mode {
			case "nil context":
				ctx = nil
			case "invalid authority":
				authority = rolejournal.Anchor{}
			case "target drift":
				authority.TargetID++
			case "profile drift":
				authority.AdminProfileID++
			case "digest drift":
				authority.ContextDigest = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
			case "database drift":
				authority.DatabaseName = "other"
			case "successor drift":
				authority.SuccessorName = "other"
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				wantErr = context.Canceled
			case "expired":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				wantErr = context.DeadlineExceeded
			case "nil journal":
				journal = nil
			case "journal read":
				wantErr = errors.New("journal read failed")
				store.afterGet = func(context.Context, resourcecontract.CredentialResource) error { return wantErr }
			default:
				wantErr = rolejournal.ErrReconciliationRequired
			}
			calls, writes := 0, 0
			store.beforeUpdate = func(rolejournal.Record) { writes++ }
			got, err := Reconcile(ctx, journal, entry, authority, func(context.Context) (Connection, error) { calls++; return connection, nil })
			if err == nil || (wantErr != nil && !errors.Is(err, wantErr)) || got != (rolejournal.Entry{}) || calls != 0 || writes != 0 ||
				store.last != entry.Record || len(tx.statements) != 0 || tx.commits != 0 || tx.rollbacks != 0 || connection.closes != 0 {
				t.Fatalf("ineligible request dispatched: got=%#v err=%v calls/writes=%d/%d", got, err, calls, writes)
			}
		})
	}
}

func TestReconcileRemoteVerificationFailuresNeverTransition(t *testing.T) {
	for _, status := range []rolejournal.Status{rolejournal.ProvisionIntent, rolejournal.CleanupIntent} {
		for _, mode := range []string{"fence", "cluster", "database OID", "database name", "successor OID", "successor name", "role OID", "role name", "marker", "missing marker", "absent role", "anchor read", "role read"} {
			t.Run(string(status)+"/"+mode, func(t *testing.T) {
				journal, store, tx, connection, entry := reconciliationFixture(t, status, true)
				sentinel, wantErr := errors.New("remote verification failed"), ErrIdentityDrift
				switch mode {
				case "fence":
					tx.lockErr, wantErr = sentinel, sentinel
				case "anchor read", "role read":
					query := anchorQuery
					if mode == "role read" {
						query = roleQuery
					}
					tx.rows[query], wantErr = func(...any) error { return sentinel }, sentinel
				case "absent role":
					tx.rows[roleQuery] = func(...any) error { return pgx.ErrNoRows }
				default:
					query, index, value := anchorQuery, 0, any("1")
					switch mode {
					case "database OID":
						index, value = 1, uint32(99)
					case "database name":
						index, value = 2, "Main DB"
					case "successor OID":
						index, value = 3, uint32(99)
					case "successor name":
						index, value = 4, "Main Admin"
					case "role OID":
						query, index, value = roleQuery, 0, uint32(99)
					case "role name":
						query, index, value = roleQuery, 1, "My Role"
					case "marker":
						query, index, value = roleQuery, 2, "another marker"
					case "missing marker":
						query, index, value = roleQuery, 2, ""
					}
					original := tx.rows[query]
					tx.rows[query] = func(destination ...any) error {
						if err := original(destination...); err != nil {
							return err
						}
						switch v := value.(type) {
						case string:
							*destination[index].(*string) = v
						case uint32:
							*destination[index].(*uint32) = v
						}
						return nil
					}
				}
				calls, writes := 0, 0
				store.beforeUpdate = func(rolejournal.Record) { writes++ }
				got, err := Reconcile(t.Context(), journal, entry, entry.Record.Intent.Anchor,
					func(context.Context) (Connection, error) { calls++; return connection, nil })
				fresh, readErr := journal.Get(t.Context(), entry.ResourceID)
				if !errors.Is(err, wantErr) || connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown || got != (rolejournal.Entry{}) ||
					calls != 1 || writes != 0 || readErr != nil || fresh != entry {
					t.Fatalf("failed verification changed state: got=%#v fresh=%#v err=%v readErr=%v calls/writes=%d/%d", got, fresh, err, readErr, calls, writes)
				}
				if mode == "fence" && len(tx.queries) != 0 {
					t.Fatal("failed fresh fence permitted remote identity reads")
				}
				assertReconciliationReleased(t, tx, connection)
			})
		}
	}
}

func TestReconcileConfirmationFailuresPreserveFreshState(t *testing.T) {
	for _, status := range []rolejournal.Status{rolejournal.ProvisionIntent, rolejournal.CleanupIntent} {
		for _, mode := range []string{"write before", "write after", "readback"} {
			t.Run(string(status)+"/"+mode, func(t *testing.T) {
				journal, store, tx, connection, entry := reconciliationFixture(t, status, true)
				store.failStatus, store.afterWrite = rolejournal.Provisioned, mode == "write after"
				sentinel := errors.New("confirmation readback failed")
				if mode == "readback" {
					store.failStatus = ""
					store.afterGet = func(_ context.Context, _ resourcecontract.CredentialResource) error {
						if store.last.Status == rolejournal.Provisioned {
							return sentinel
						}
						return nil
					}
				}
				calls, writes := 0, 0
				store.beforeUpdate = func(rolejournal.Record) { writes++ }
				got, err := Reconcile(t.Context(), journal, entry, entry.Record.Intent.Anchor,
					func(context.Context) (Connection, error) { calls++; return connection, nil })
				if got != (rolejournal.Entry{}) || calls != 1 || writes != 1 || (mode == "readback" && !errors.Is(err, sentinel)) {
					t.Fatalf("unconfirmed result=%#v err=%v calls/writes=%d/%d", got, err, calls, writes)
				}
				assertReconciliationUnknown(t, err, entry)
				store.afterGet = nil
				fresh, readErr := journal.Get(t.Context(), entry.ResourceID)
				want := entry
				if mode != "write before" {
					want.Record.Status, want.Record.Generation = rolejournal.Provisioned, fresh.Record.Generation
					if fresh.Record.Generation == entry.Record.Generation {
						t.Fatal("persisted confirmation lost its new generation")
					}
				}
				if readErr != nil || fresh != want {
					t.Fatalf("confirmation failure overwrote durable state: fresh=%#v want=%#v err=%v", fresh, want, readErr)
				}
				assertReconciliationReleased(t, tx, connection)
			})
		}
	}
}

func TestReconcileRejectsGenerationChangedDuringRemoteVerification(t *testing.T) {
	for _, status := range []rolejournal.Status{rolejournal.ProvisionIntent, rolejournal.CleanupIntent} {
		t.Run(string(status), func(t *testing.T) {
			journal, store, tx, connection, entry := reconciliationFixture(t, status, true)
			var changed rolejournal.Entry
			writes := 0
			original := tx.rows[roleQuery]
			tx.rows[roleQuery] = func(destination ...any) error {
				if err := original(destination...); err != nil {
					return err
				}
				transition := journal.ConfirmProvision
				if status == rolejournal.CleanupIntent {
					transition = journal.ConfirmCleanupNotApplied
				}
				var err error
				changed, err = transition(t.Context(), entry)
				if err == nil {
					changed, _, err = journal.BeginCleanup(t.Context(), changed)
				}
				if err != nil {
					t.Fatal(err)
				}
				store.beforeUpdate = func(rolejournal.Record) { writes++ }
				return nil
			}
			got, err := Reconcile(t.Context(), journal, entry, entry.Record.Intent.Anchor,
				func(context.Context) (Connection, error) { return connection, nil })
			fresh, readErr := journal.Get(t.Context(), entry.ResourceID)
			if !errors.Is(err, rolejournal.ErrStaleGeneration) || got != (rolejournal.Entry{}) || writes != 0 ||
				readErr != nil || fresh != changed || changed.Record.Generation == entry.Record.Generation {
				t.Fatalf("remote verification allowed stale transition: got=%#v fresh=%#v changed=%#v err=%v", got, fresh, changed, err)
			}
			assertReconciliationUnknown(t, err, entry)
			assertReconciliationReleased(t, tx, connection)
		})
	}
}

func TestReconcileRejectsTypedNilRemoteDependencies(t *testing.T) {
	for _, mode := range []string{"connection", "transaction"} {
		t.Run(mode, func(t *testing.T) {
			journal, store, tx, connection, entry := reconciliationFixture(t, rolejournal.ProvisionIntent, true)
			var remote Connection = connection
			wantCloses := 1
			if mode == "connection" {
				var missing *lifecycleConnection
				remote, wantCloses = missing, 0
			} else {
				var missing *lifecycleTx
				connection.starter.tx = missing
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("typed-nil %s admission panicked instead of returning an error: %v", mode, recovered)
				}
			}()
			calls, writes := 0, 0
			store.beforeUpdate = func(rolejournal.Record) { writes++ }
			got, err := Reconcile(t.Context(), journal, entry, entry.Record.Intent.Anchor,
				func(context.Context) (Connection, error) { calls++; return remote, nil })
			if err == nil || got != (rolejournal.Entry{}) || connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown ||
				calls != 1 || writes != 0 || store.last != entry.Record || len(tx.statements) != 0 ||
				tx.commits != 0 || tx.rollbacks != 0 || connection.closes != wantCloses {
				t.Fatalf("typed-nil %s admitted: got=%#v err=%v calls/writes=%d/%d closes=%d", mode, got, err, calls, writes, connection.closes)
			}
			if wantCloses != 0 && (connection.closeCanceled || connection.closeDeadline <= 0 || connection.closeDeadline > 5*time.Second) {
				t.Fatal("typed-nil transaction leaked unbounded connection cleanup")
			}
		})
	}
}

func TestReconcileBoundsOperationAndDetachesCleanup(t *testing.T) {
	for _, mode := range []string{"default deadline", "shorter caller deadline", "cancel during verification"} {
		t.Run(mode, func(t *testing.T) {
			journal, store, tx, connection, entry := reconciliationFixture(t, rolejournal.CleanupIntent, true)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "shorter caller deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 10*time.Second)
				defer deadlineCancel()
			}
			var operationCtx context.Context
			checkDeadline := func(ctx context.Context) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > lifecycleTimeout {
					t.Fatal("reconciliation operation lacks a bounded deadline")
				}
				if parentDeadline, ok := operationCtx.Deadline(); ok && deadline != parentDeadline {
					t.Fatal("journal confirmation changed the operation deadline")
				}
			}
			store.afterGet = func(ctx context.Context, _ resourcecontract.CredentialResource) error {
				if operationCtx != nil {
					checkDeadline(ctx)
				}
				return nil
			}
			if mode == "cancel during verification" {
				original := tx.rows[roleQuery]
				tx.rows[roleQuery] = func(destination ...any) error {
					err := original(destination...)
					cancel()
					return err
				}
			}
			got, err := Reconcile(ctx, journal, entry, entry.Record.Intent.Anchor, func(dialCtx context.Context) (Connection, error) {
				operationCtx = dialCtx
				checkDeadline(dialCtx)
				if deadline, ok := ctx.Deadline(); ok {
					actual, _ := dialCtx.Deadline()
					if actual != deadline {
						t.Fatal("reconciliation extended caller deadline")
					}
				}
				return connection, nil
			})
			if mode == "cancel during verification" {
				if !errors.Is(err, context.Canceled) || got != (rolejournal.Entry{}) || store.last != entry.Record {
					t.Fatalf("canceled verification confirmed: got=%#v err=%v", got, err)
				}
				assertReconciliationUnknown(t, err, entry)
			} else if err != nil || got.Record.Status != rolejournal.Provisioned {
				t.Fatalf("bounded reconciliation failed: got=%#v err=%v", got, err)
			}
			if operationCtx == nil || operationCtx.Err() == nil {
				t.Fatal("operation context was not canceled on return")
			}
			assertReconciliationReleased(t, tx, connection)
		})
	}
}
