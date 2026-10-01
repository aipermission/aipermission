package rolecatalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

func TestCleanupConfirmsOnlyAcknowledgedNondestructiveTransaction(t *testing.T) {
	journal, store, tx, connection := lifecycleFixture(t)
	entry := provisionFixture(t, journal, connection)
	tx.statements, tx.commits = nil, 0
	tx.beforeExec = func(sql string) {
		if sql != catalogLock && !strings.HasPrefix(sql, "SELECT") && !strings.HasPrefix(sql, "SET LOCAL") && store.last.Status != rolejournal.CleanupIntent {
			t.Fatal("cleanup mutation preceded durable intent")
		}
		if strings.Contains(sql, "DROP OWNED") {
			t.Fatal("cleanup could delete late-owned objects")
		}
	}
	finished, err := Cleanup(t.Context(), journal, entry, func(context.Context) (Connection, error) { return connection, nil })
	if err != nil || finished.Record.Status != rolejournal.Cleaned || finished.Reference() != entry.Reference() || tx.commits != 1 {
		t.Fatalf("confirmed cleanup=%#v %v", finished, err)
	}
	if got, err := Cleanup(t.Context(), journal, finished, nil); err != nil || got != finished {
		t.Fatalf("fresh durable confirmation did not skip remote dispatch: %#v %v", got, err)
	}
	stale := finished
	stale.Record.Generation = entry.Record.Generation
	if _, err := Cleanup(t.Context(), journal, stale, nil); !errors.Is(err, rolejournal.ErrStaleGeneration) {
		t.Fatalf("stale terminal record accepted: %v", err)
	}
}

func TestCleanupFailureRetainsProfileAndDoesNotInferSuccessFromAbsence(t *testing.T) {
	for _, mode := range []string{"identity", "intent before", "intent after", "statement", "rollback", "rollback confirmation", "commit rollback", "commit rollback confirmation", "commit unknown", "confirmation"} {
		t.Run(mode, func(t *testing.T) {
			journal, store, tx, connection := lifecycleFixture(t)
			entry := provisionFixture(t, journal, connection)
			tx.statements, tx.commits = nil, 0
			sentinel := errors.New("injected cleanup failure")
			unknown := false
			switch mode {
			case "identity":
				tx.rows[roleQuery] = func(...any) error { return pgx.ErrNoRows }
			case "intent before":
				store.failStatus = rolejournal.CleanupIntent
			case "intent after":
				store.failStatus, store.afterWrite = rolejournal.CleanupIntent, true
			case "statement":
				tx.execErr = sentinel
			case "rollback":
				tx.execErr, tx.rollbackErr, unknown = sentinel, sentinel, true
			case "rollback confirmation":
				tx.execErr, store.failStatus, unknown = sentinel, rolejournal.Provisioned, true
			case "commit rollback":
				tx.commitErr = pgx.ErrTxCommitRollback
			case "commit rollback confirmation":
				tx.commitErr, store.failStatus, unknown = pgx.ErrTxCommitRollback, rolejournal.Provisioned, true
			case "commit unknown":
				tx.commitErr, unknown = sentinel, true
			case "confirmation":
				store.failStatus, unknown = rolejournal.Cleaned, true
			}
			calls := 0
			got, err := Cleanup(t.Context(), journal, entry, func(context.Context) (Connection, error) { calls++; return connection, nil })
			if err == nil || got.ResourceID != 0 || (connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown) != unknown || calls != 1 {
				t.Fatalf("unsafe cleanup result=%#v err=%v calls=%d", got, err, calls)
			}
			if mode == "identity" || strings.HasPrefix(mode, "intent") {
				if len(mutationStatements(tx)) != 0 || tx.commits != 0 {
					t.Fatal("identity or persistence failure authorized cleanup mutation")
				}
			}
			if mode == "statement" || mode == "commit rollback" {
				if store.last.Status != rolejournal.Provisioned {
					t.Fatal("acknowledged cleanup rollback lost retryable provisioned state")
				}
			} else if store.last.Status == rolejournal.Cleaned {
				t.Fatal("uncertain cleanup falsely confirmed")
			}
		})
	}
}

func TestCleanupRejectsMissingJournalOrUnresolvedState(t *testing.T) {
	if _, err := Cleanup(t.Context(), nil, rolejournal.Entry{}, nil); err == nil {
		t.Fatal("missing journal accepted")
	}
	for _, status := range []rolejournal.Status{rolejournal.ProvisionIntent, rolejournal.CleanupIntent, rolejournal.RolledBack} {
		entry := rolejournal.Entry{Record: testRecord()}
		entry.Record.Status = status
		if _, err := Cleanup(t.Context(), rolejournal.New(nil), entry, nil); !errors.Is(err, rolejournal.ErrReconciliationRequired) {
			t.Fatalf("unresolved state accepted: %v", err)
		}
	}
}
