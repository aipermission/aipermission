package rolecatalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

func TestProvisionOrdersDurableIntentBindingAndAcknowledgement(t *testing.T) {
	journal, store, tx, connection := lifecycleFixture(t)
	tx.beforeExec = func(sql string) {
		if sql != catalogLock && (store.last.Status != rolejournal.ProvisionIntent || store.last.RoleOID != 0) {
			t.Fatalf("mutation preceded durable intent: %q %#v", sql, store.last)
		}
	}
	tx.beforeCommit = func() {
		if store.last.Status != rolejournal.ProvisionIntent || store.last.RoleOID != 42 {
			t.Fatal("remote commit preceded durable exact role binding")
		}
	}
	entry := provisionFixture(t, journal, connection)
	if entry.Record.Status != rolejournal.Provisioned || entry.Record.RoleOID != 42 || tx.commits != 1 {
		t.Fatalf("provisioned=%#v commits=%d", entry, tx.commits)
	}
	if connection.closes == 0 || connection.closeCanceled || connection.closeDeadline <= 0 || connection.closeDeadline > 5*time.Second {
		t.Fatal("connection cleanup was not detached and bounded")
	}
}

func TestProvisionFailuresNeverAuthorizeBlindRedispatch(t *testing.T) {
	for _, mode := range []string{"intent", "statement", "role read", "binding before", "binding after", "rollback", "rollback confirmation", "commit rollback", "commit rollback confirmation", "commit unknown", "confirmation"} {
		t.Run(mode, func(t *testing.T) {
			journal, store, tx, connection := lifecycleFixture(t)
			sentinel := errors.New("injected lifecycle failure")
			unknown := false
			switch mode {
			case "intent":
				store.createErr = sentinel
			case "statement":
				tx.execErr = sentinel
			case "role read":
				tx.rows[roleQuery] = func(...any) error { return sentinel }
			case "binding before":
				store.failBinding = true
			case "binding after":
				store.failBinding, store.afterWrite, unknown = true, true, true
			case "rollback":
				tx.execErr, tx.rollbackErr, unknown = sentinel, sentinel, true
			case "rollback confirmation":
				tx.execErr, store.failStatus, unknown = sentinel, rolejournal.RolledBack, true
			case "commit rollback":
				tx.commitErr = pgx.ErrTxCommitRollback
			case "commit rollback confirmation":
				tx.commitErr, store.failStatus, unknown = pgx.ErrTxCommitRollback, rolejournal.RolledBack, true
			case "commit unknown":
				tx.commitErr, unknown = sentinel, true
			case "confirmation":
				store.failStatus, unknown = rolejournal.Provisioned, true
			}
			calls := 0
			entry, err := Provision(t.Context(), journal, testRecord().Intent.Anchor, " My Role ", []string{`CREATE ROLE " My Role "`},
				func(context.Context) (Connection, error) {
					calls++
					if calls > 1 {
						return nil, sentinel
					}
					return connection, nil
				})
			if err == nil || entry.ResourceID != 0 || (connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown) != unknown {
				t.Fatalf("unsafe result=%#v err=%v status=%s", entry, err, connectors.ErrorStatus(err))
			}
			if mode == "intent" && len(mutationStatements(tx)) != 0 {
				t.Fatal("missing durable intent authorized CREATE ROLE")
			}
			if unknown && store.row.ID != 0 {
				store.createErr, store.failStatus, store.failBinding = nil, "", false
				if _, err := journal.BeginProvision(t.Context(), store.last.Intent.Anchor, " My Role "); !errors.Is(err, rolejournal.ErrReconciliationRequired) {
					t.Fatalf("uncertainty allowed new provisioning dispatch: %v", err)
				}
			}
		})
	}
}

func TestProvisionAmbiguousCommitUsesExactFencedObservation(t *testing.T) {
	journal, store, tx, first := lifecycleFixture(t)
	tx.commitErr = errors.New("commit acknowledgement lost")
	secondTx := &lifecycleTx{transaction: cleanupTransaction(t)}
	secondTx.rows[roleQuery] = tx.rows[roleQuery]
	second := &lifecycleConnection{starter: &starter{tx: secondTx}}
	confirmations := 0
	store.beforeUpdate = func(next rolejournal.Record) {
		if next.Status == rolejournal.Provisioned {
			confirmations++
			if first.closes == 0 || second.closes != 0 || secondTx.rollbacks != 0 || secondTx.commits != 0 {
				t.Fatal("ambiguous COMMIT released its fresh observation fence before durable confirmation")
			}
		}
	}
	calls := 0
	entry, err := Provision(t.Context(), journal, testRecord().Intent.Anchor, " My Role ", []string{`CREATE ROLE " My Role "`},
		func(context.Context) (Connection, error) {
			calls++
			if calls == 1 {
				return first, nil
			}
			return second, nil
		})
	if err != nil || entry.Record.Status != rolejournal.Provisioned || store.last.RoleOID != 42 || calls != 2 || confirmations != 1 {
		t.Fatalf("exact commit observation=%#v %v calls=%d", entry, err, calls)
	}
	if len(mutationStatements(secondTx)) != 0 || !strings.Contains(strings.Join(secondTx.statements, "\n"), "pg_catalog.pg_authid") {
		t.Fatal("commit recovery dispatched mutations or lacked a fresh identity fence")
	}
}
