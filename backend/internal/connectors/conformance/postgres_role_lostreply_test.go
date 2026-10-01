package conformance_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

func TestPostgresRoleProvisionLostCommitReplyRequiresExplicitReconciliationRealService(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresLostReplyFixture(t)
	wire := &postgresLostReplyWire{armed: true, readIntent: fixture.journal.List}
	dial := wire.dial(t, fixture.primary.Config())
	result, err := rolecatalog.Provision(t.Context(), fixture.journal, fixture.authority, fixture.role, fixture.plan(), dial)
	intent := assertPostgresLostCommitIntent(t, fixture, wire, result, err, rolejournal.ProvisionIntent)
	wire.mu.Lock()
	dials, blocked := wire.dials, wire.blocked
	wire.mu.Unlock()
	if dials != 2 || blocked != 1 {
		t.Fatalf("provision did not attempt exactly one read-only observation after commit loss: dials=%d blocked=%d", dials, blocked)
	}
	mutations := wire.mutations()
	wantMutations := append(fixture.plan(), "COMMENT ON ROLE "+pgx.Identifier{fixture.role}.Sanitize()+" IS '"+intent.Record.Intent.Marker()+"'")
	if !reflect.DeepEqual(mutations, wantMutations) {
		t.Fatalf("provision replayed or cleaned up after a lost reply: %q", mutations)
	}
	assertPostgresLostReplyRemoteState(t, fixture, intent.Record.RoleOID, fixture.role)

	fixture.journal = fixture.reopen()
	assertPostgresLostReplyJournalUnchanged(t, fixture.journal, intent)
	wire.restore()
	result, err = rolecatalog.Cleanup(t.Context(), fixture.journal, intent, dial)
	if result != (rolejournal.Entry{}) || !errors.Is(err, rolejournal.ErrReconciliationRequired) {
		t.Fatalf("unresolved provision allowed cleanup: %#v %v", result, err)
	}
	wire.mu.Lock()
	cleanupDials := wire.dials
	wire.mu.Unlock()
	if cleanupDials != dials {
		t.Fatal("unresolved provision dispatched automatic cleanup")
	}

	result, err = rolecatalog.Provision(t.Context(), fixture.journal, fixture.authority, fixture.role, fixture.plan(), dial)
	if result != (rolejournal.Entry{}) || !errors.Is(err, rolejournal.ErrReconciliationRequired) {
		t.Fatalf("unresolved provision allowed replay: %#v %v", result, err)
	}
	assertPostgresLostReplyJournalUnchanged(t, fixture.journal, intent)
	if !reflect.DeepEqual(wire.mutations(), mutations) {
		t.Fatal("retry mutated the unresolved remote role")
	}

	confirmed, err := rolecatalog.Reconcile(t.Context(), fixture.journal, intent, fixture.authority, dial)
	if err != nil || confirmed.Record.Status != rolejournal.Provisioned || confirmed.Reference() != intent.Reference() ||
		confirmed.Record.RoleOID != intent.Record.RoleOID || confirmed.Record.Generation == intent.Record.Generation {
		t.Fatalf("explicit reconciliation did not confirm the exact committed identity: %#v %v", confirmed, err)
	}
	fixture.journal = fixture.reopen()
	assertPostgresLostReplyJournalUnchanged(t, fixture.journal, confirmed)
	assertPostgresLostReplyRemoteState(t, fixture, intent.Record.RoleOID, fixture.role)
	if !reflect.DeepEqual(wire.mutations(), mutations) {
		t.Fatal("explicit reconciliation dispatched remote mutations")
	}

	cleaned, err := rolecatalog.Cleanup(t.Context(), fixture.journal, confirmed, dial)
	if err != nil || cleaned.Record.Status != rolejournal.Cleaned {
		t.Fatalf("explicit cleanup after reconciliation: %#v %v", cleaned, err)
	}
	assertPostgresLostReplyRemoteState(t, fixture, 0, "aipermission")
}

func TestPostgresRoleCleanupLostCommitReplyRetainsIntentWithoutReplayRealService(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresLostReplyFixture(t)
	setup := &postgresLostReplyWire{}
	provisioned, err := rolecatalog.Provision(t.Context(), fixture.journal, fixture.authority, fixture.role, fixture.plan(), setup.dial(t, fixture.primary.Config()))
	if err != nil {
		t.Fatal(err)
	}
	assertPostgresLostReplyRemoteState(t, fixture, provisioned.Record.RoleOID, fixture.role)
	wire := &postgresLostReplyWire{armed: true, readIntent: fixture.journal.List}
	dial := wire.dial(t, fixture.primary.Config())
	result, err := rolecatalog.Cleanup(t.Context(), fixture.journal, provisioned, dial)
	intent := assertPostgresLostCommitIntent(t, fixture, wire, result, err, rolejournal.CleanupIntent)
	if intent.Reference() != provisioned.Reference() || intent.Record.RoleOID != provisioned.Record.RoleOID || intent.Record.Generation == provisioned.Record.Generation {
		t.Fatal("cleanup lost its durable role binding or intent generation")
	}
	wire.mu.Lock()
	dials, blocked := wire.dials, wire.blocked
	wire.mu.Unlock()
	if dials != 1 || blocked != 0 {
		t.Fatal("cleanup automatically retried or reconciled its lost COMMIT")
	}
	mutations := wire.mutations()
	wantMutations := []string{
		"REASSIGN OWNED BY " + pgx.Identifier{fixture.role}.Sanitize() + " TO " + pgx.Identifier{"aipermission"}.Sanitize(),
		"REVOKE ALL PRIVILEGES ON table " + pgx.Identifier{"public", fixture.table}.Sanitize() + " FROM " + pgx.Identifier{fixture.role}.Sanitize() + " CASCADE",
		"DROP ROLE " + pgx.Identifier{fixture.role}.Sanitize(),
	}
	if len(mutations) != len(wantMutations) {
		t.Fatalf("cleanup replayed or sent unexpected mutations: %q", mutations)
	}
	for _, statement := range wantMutations {
		count := 0
		for _, actual := range mutations {
			if actual == statement {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("cleanup statement not sent exactly once: %q in %q", statement, mutations)
		}
	}
	assertPostgresLostReplyRemoteState(t, fixture, 0, "aipermission")
	fixture.journal = fixture.reopen()
	assertPostgresLostReplyJournalUnchanged(t, fixture.journal, intent)
	wire.restore()
	result, err = rolecatalog.Cleanup(t.Context(), fixture.journal, intent, dial)
	if result != (rolejournal.Entry{}) || !errors.Is(err, rolejournal.ErrReconciliationRequired) {
		t.Fatalf("unknown cleanup replayed: %#v %v", result, err)
	}
	wire.mu.Lock()
	retryDials := wire.dials
	wire.mu.Unlock()
	if retryDials != dials {
		t.Fatal("unknown cleanup opened a replay connection")
	}
	result, err = rolecatalog.Reconcile(t.Context(), fixture.journal, intent, fixture.authority, dial)
	if result != (rolejournal.Entry{}) || !errors.Is(err, rolecatalog.ErrIdentityDrift) {
		t.Fatalf("name absence incorrectly proved successful cleanup: %#v %v", result, err)
	}
	fixture.journal = fixture.reopen()
	assertPostgresLostReplyJournalUnchanged(t, fixture.journal, intent)
	if !reflect.DeepEqual(wire.mutations(), mutations) {
		t.Fatal("cleanup retry/reconciliation dispatched mutations")
	}
	assertPostgresLostReplyRemoteState(t, fixture, 0, "aipermission")
}

func assertPostgresLostCommitIntent(t *testing.T, fixture postgresLostReplyFixture, wire *postgresLostReplyWire, result rolejournal.Entry, err error, status rolejournal.Status) rolejournal.Entry {
	t.Helper()
	if result != (rolejournal.Entry{}) || connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
		t.Fatalf("lost wire reply did not report unknown outcome: %#v %v", result, err)
	}
	entries, readErr := fixture.journal.List(t.Context())
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("durable intent missing: %#v %v", entries, readErr)
	}
	intent := entries[0]
	wire.mu.Lock()
	accepted, before, beforeErr := wire.accepted, append([]rolejournal.Entry(nil), wire.beforeCommit...), wire.readErr
	wire.mu.Unlock()
	if accepted != 1 || beforeErr != nil || len(before) != 1 || before[0] != intent || intent.Record.Status != status || intent.Record.RoleOID == 0 {
		t.Fatalf("missing real accepted COMMIT or pre-dispatch durable binding: accepted=%d before=%#v read=%v intent=%#v", accepted, before, beforeErr, intent)
	}
	details := connectors.ErrorDetails(err)
	if details["dispatch_stage"] != "transaction_commit" || details["retry_safe"] != false || details["reconciliation_required"] != true ||
		details["journal_resource_id"] != intent.Reference().ResourceID || details["journal_generation"] != intent.Record.Generation || details["journal_status"] != string(status) {
		t.Fatalf("unknown outcome lost its no-retry contract or durable reference: %#v", details)
	}
	return intent
}

func assertPostgresLostReplyJournalUnchanged(t *testing.T, journal *rolejournal.Journal, expected rolejournal.Entry) {
	t.Helper()
	fresh, err := journal.Get(t.Context(), expected.ResourceID)
	if err != nil || fresh != expected {
		t.Fatalf("durable intent/decision changed: %#v %v want=%#v", fresh, err, expected)
	}
}

func assertPostgresLostReplyRemoteState(t *testing.T, fixture postgresLostReplyFixture, roleOID uint32, owner string) {
	t.Helper()
	var oid uint32
	var marker string
	err := fixture.primary.QueryRow(t.Context(), `SELECT oid, coalesce(pg_catalog.shobj_description(oid, 'pg_authid'), '') FROM pg_catalog.pg_roles WHERE rolname = $1`, fixture.role).Scan(&oid, &marker)
	if roleOID == 0 {
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("committed cleanup retained the role: oid=%d err=%v", oid, err)
		}
	} else {
		entries, readErr := fixture.journal.List(t.Context())
		if err != nil || oid != roleOID || readErr != nil || len(entries) != 1 || marker != entries[0].Record.Intent.Marker() {
			t.Fatalf("real committed role identity mismatch: oid=%d marker=%q err=%v journal=%v", oid, marker, err, readErr)
		}
	}
	var actualOwner string
	var value int
	if err := fixture.primary.QueryRow(t.Context(), `SELECT pg_catalog.pg_get_userbyid(relowner) FROM pg_catalog.pg_class WHERE oid = $1::regclass`, pgx.Identifier{"public", fixture.table}.Sanitize()).Scan(&actualOwner); err != nil || actualOwner != owner {
		t.Fatalf("committed ownership mismatch: owner=%q err=%v", actualOwner, err)
	}
	if err := fixture.primary.QueryRow(t.Context(), "SELECT id FROM "+pgx.Identifier{"public", fixture.table}.Sanitize()).Scan(&value); err != nil || value != 42 {
		t.Fatalf("lifecycle lost owned data: value=%d err=%v", value, err)
	}
}
