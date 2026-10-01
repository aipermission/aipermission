package conformance_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorresources"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresRoleReconciliationRetainsFenceThroughDurableReadback(t *testing.T) {
	requireConformance(t)
	for _, outcome := range []string{"provision committed", "cleanup rolled back", "cleanup committed"} {
		t.Run(outcome, func(t *testing.T) {
			fixture := newPostgresRoleFenceFixture(t)
			store := newPostgresReconciliationStore(t)
			journal := rolejournal.New(store)
			entry := preparePostgresReconciliation(t, fixture, journal, outcome, nil)
			before := entry
			store.checkFence = func() { assertPostgresReconciliationWriterBlocked(t, fixture) }
			var connection *pgx.Conn
			confirmed, err := rolecatalog.Reconcile(t.Context(), journal, entry, entry.Record.Intent.Anchor,
				func(ctx context.Context) (rolecatalog.Connection, error) {
					var dialErr error
					connection, dialErr = pgx.ConnectConfig(ctx, fixture.config.Copy())
					return connection, dialErr
				})
			if connection != nil && !connection.IsClosed() {
				t.Fatal("reconciliation retained its PostgreSQL connection")
			}
			fresh, readErr := journal.Get(t.Context(), entry.ResourceID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if outcome == "cleanup committed" {
				if !errors.Is(err, rolecatalog.ErrIdentityDrift) || confirmed.ResourceID != 0 || fresh != before || store.checks != 0 {
					t.Fatalf("absence authorized a durable decision: %#v %v fresh=%#v checks=%d", confirmed, err, fresh, store.checks)
				}
				return
			}
			if err != nil || confirmed.Record.Status != rolejournal.Provisioned || fresh != confirmed ||
				confirmed.Reference() != before.Reference() || confirmed.Record.Generation == before.Record.Generation || store.checks != 2 {
				t.Fatalf("fenced reconciliation failed: %#v %v fresh=%#v checks=%d", confirmed, err, fresh, store.checks)
			}
			assertPostgresReconciliationRolePreserved(t, fixture, confirmed)
		})
	}
}

func TestPostgresRoleReconciliationRejectsAnActiveCleanupTransaction(t *testing.T) {
	requireConformance(t)
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[committed], func(t *testing.T) {
			fixture := newPostgresRoleFenceFixture(t)
			store := newPostgresReconciliationStore(t)
			journal := rolejournal.New(store)
			entry := preparePostgresReconciliation(t, fixture, journal, "cleanup rolled back", nil)
			writer := beginPostgresRoleFence(t, fixture.primary)
			plan, err := rolecatalog.CleanupPlan(t.Context(), writer, entry.Record)
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range plan {
				if _, err := writer.Exec(t.Context(), statement); err != nil {
					t.Fatal(err)
				}
			}
			dial := func(ctx context.Context) (rolecatalog.Connection, error) {
				return pgx.ConnectConfig(ctx, fixture.config.Copy())
			}
			confirmed, err := rolecatalog.Reconcile(t.Context(), journal, entry, entry.Record.Intent.Anchor, dial)
			var pgError *pgconn.PgError
			if !errors.As(err, &pgError) || pgError.Code != "55P03" || confirmed.ResourceID != 0 {
				t.Fatalf("active original cleanup allowed reconciliation: %#v %v", confirmed, err)
			}
			fresh, err := journal.Get(t.Context(), entry.ResourceID)
			if err != nil || fresh != entry {
				t.Fatalf("failed admission changed durable intent: %#v %v", fresh, err)
			}
			if committed {
				err = writer.Commit(t.Context())
			} else {
				err = writer.Rollback(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			confirmed, err = rolecatalog.Reconcile(t.Context(), journal, entry, entry.Record.Intent.Anchor, dial)
			if committed {
				fresh, readErr := journal.Get(t.Context(), entry.ResourceID)
				if !errors.Is(err, rolecatalog.ErrIdentityDrift) || confirmed.ResourceID != 0 || readErr != nil || fresh != entry {
					t.Fatalf("committed cleanup absence accepted: %#v %v fresh=%#v %v", confirmed, err, fresh, readErr)
				}
			} else if err != nil || confirmed.Record.Status != rolejournal.Provisioned || confirmed.Reference() != entry.Reference() {
				t.Fatalf("resolved rollback could not reconcile: %#v %v", confirmed, err)
			}
		})
	}
}

type postgresReconciliationStore struct {
	resourcecontract.CredentialResourceStore
	checkFence func()
	readback   bool
	checks     int
}

func newPostgresReconciliationStore(t *testing.T) *postgresReconciliationStore {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "private", "role-reconciliation.db"), "role-reconciliation-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	secretVault, err := vault.New("role-reconciliation-fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	resources := connectorresources.NewStore(database, secretVault, "role-reconciliation-fixture-workspace")
	return &postgresReconciliationStore{CredentialResourceStore: resources.Scope("postgres", rolejournal.ResourceKind)}
}

func (store *postgresReconciliationStore) Update(ctx context.Context, id int64, input resourcecontract.UpdateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if store.checkFence != nil {
		store.checkFence()
		store.checks++
		store.readback = true
	}
	return store.CredentialResourceStore.Update(ctx, id, input)
}

func (store *postgresReconciliationStore) Get(ctx context.Context, id int64) (resourcecontract.CredentialResource, error) {
	resource, err := store.CredentialResourceStore.Get(ctx, id)
	if store.readback && store.checkFence != nil {
		store.checkFence()
		store.checks++
		store.readback = false
	}
	return resource, err
}

func preparePostgresReconciliation(t *testing.T, fixture postgresRoleFenceFixture, journal *rolejournal.Journal, outcome string, authority *rolejournal.Anchor) rolejournal.Entry {
	t.Helper()
	tx := beginPostgresRoleFence(t, fixture.primary)
	record := fixture.record(t, tx)
	if authority != nil {
		if authority.DatabaseName != record.Intent.Anchor.DatabaseName || authority.SuccessorName != record.Intent.Anchor.SuccessorName {
			t.Fatal("operator fixture authority does not match the remote database/admin")
		}
		record.Intent.Anchor.TargetID, record.Intent.Anchor.AdminProfileID = authority.TargetID, authority.AdminProfileID
		record.Intent.Anchor.ContextDigest = authority.ContextDigest
		record.Intent.Anchor.TargetDigest = authority.TargetDigest
	}
	entry, err := journal.BeginProvision(t.Context(), record.Intent.Anchor, fixture.role)
	if err != nil {
		t.Fatal(err)
	}
	markerSQL := "COMMENT ON ROLE " + pgx.Identifier{fixture.role}.Sanitize() + " IS '" + strings.ReplaceAll(entry.Record.Intent.Marker(), "'", "''") + "'"
	if _, err := tx.Exec(t.Context(), markerSQL); err != nil {
		t.Fatal(err)
	}
	entry, err = journal.BindRole(t.Context(), entry, record.RoleOID)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if outcome == "provision committed" {
		return entry
	}
	entry, err = journal.ConfirmProvision(t.Context(), entry)
	if err != nil {
		t.Fatal(err)
	}
	tx = beginPostgresRoleFence(t, fixture.primary)
	plan, err := rolecatalog.CleanupPlan(t.Context(), tx, entry.Record)
	if err != nil {
		t.Fatal(err)
	}
	var dispatch bool
	entry, dispatch, err = journal.BeginCleanup(t.Context(), entry)
	if err != nil || !dispatch {
		t.Fatalf("native cleanup intent not persisted: %v %v", dispatch, err)
	}
	for _, statement := range plan {
		if _, err := tx.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if outcome == "cleanup rolled back" {
		err = tx.Rollback(t.Context())
	} else {
		err = tx.Commit(t.Context())
	}
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func assertPostgresReconciliationWriterBlocked(t *testing.T, fixture postgresRoleFenceFixture) {
	t.Helper()
	writer, err := fixture.other.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := writer.Exec(t.Context(), "ALTER ROLE "+pgx.Identifier{fixture.role}.Sanitize()+" NOLOGIN")
	rollbackErr := writer.Rollback(t.Context())
	if rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	var pgError *pgconn.PgError
	if !errors.As(writeErr, &pgError) || pgError.Code != "55P03" {
		t.Fatalf("catalog writer did not conflict with the retained fence: %v", writeErr)
	}
	assertPostgresRoleFenceConnectionIdle(t, fixture.other)
}

func assertPostgresReconciliationRolePreserved(t *testing.T, fixture postgresRoleFenceFixture, entry rolejournal.Entry) {
	t.Helper()
	tx := beginPostgresRoleFence(t, fixture.primary)
	if err := rolecatalog.VerifyRole(t.Context(), tx, entry.Record); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
}
