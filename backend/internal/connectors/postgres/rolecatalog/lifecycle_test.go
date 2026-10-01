package rolecatalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

func TestLifecycleTransactionRejectsUnavailableOrUnfencedConnection(t *testing.T) {
	sentinel := errors.New("connection failed")
	for _, dial := range []Dial{nil,
		func(context.Context) (Connection, error) { return nil, nil },
		func(context.Context) (Connection, error) { return nil, sentinel },
	} {
		if conn, tx, err := lifecycleTransaction(t.Context(), dial); err == nil || conn != nil || tx != nil {
			t.Fatal("unavailable connection admitted")
		}
	}
	_, _, tx, connection := lifecycleFixture(t)
	tx.lockErr = sentinel
	if conn, tx, err := lifecycleTransaction(t.Context(), func(context.Context) (Connection, error) { return connection, nil }); err == nil || conn != nil || tx != nil || connection.closes != 1 {
		t.Fatal("failed catalog admission leaked connection")
	}
}

func TestLifecycleTransactionRejectsCanceledContextBeforeDial(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, cancelDeadline := context.WithDeadline(t.Context(), time.Unix(0, 0))
	defer cancelDeadline()
	for _, ctx := range []context.Context{nil, canceled, expired} {
		calls := 0
		conn, tx, err := lifecycleTransaction(ctx, func(context.Context) (Connection, error) { calls++; return nil, nil })
		if err == nil || conn != nil || tx != nil || calls != 0 {
			t.Fatalf("invalid context reached remote admission: %v calls=%d", err, calls)
		}
		if ctx != nil && !errors.Is(err, ctx.Err()) {
			t.Fatalf("context error lost: %v", err)
		}
	}
}

func TestProvisionRejectsInvalidAuthorityAndMissingPlan(t *testing.T) {
	journal, store, tx, connection := lifecycleFixture(t)
	dial := func(context.Context) (Connection, error) { return connection, nil }
	if _, err := Provision(t.Context(), journal, rolejournal.Anchor{}, "role", []string{"CREATE ROLE"}, dial); err == nil {
		t.Fatal("missing authority accepted")
	}
	if _, err := Provision(t.Context(), nil, testRecord().Intent.Anchor, "role", []string{"CREATE ROLE"}, dial); err == nil {
		t.Fatal("missing journal accepted")
	}
	if _, err := Provision(t.Context(), journal, testRecord().Intent.Anchor, "role", nil, dial); err == nil {
		t.Fatal("missing statements accepted")
	}
	tx.rows[anchorQuery] = func(...any) error { return errors.New("anchor unavailable") }
	if _, err := Provision(t.Context(), journal, testRecord().Intent.Anchor, "role", []string{"CREATE ROLE"}, dial); err == nil || store.row.ID != 0 || len(mutationStatements(tx)) != 0 {
		t.Fatal("failed identity discovery authorized mutation")
	}
}
