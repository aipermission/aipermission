package rolecatalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestBeginLocksAllCatalogsBeforeReadingIdentity(t *testing.T) {
	tx := validTransaction(t, testRecord())
	connection := &starter{tx: tx}
	got, err := Begin(t.Context(), connection)
	if err != nil || got != tx {
		t.Fatalf("begin: %v %v", got, err)
	}
	if connection.options.IsoLevel != pgx.ReadCommitted || connection.options.AccessMode != pgx.ReadWrite {
		t.Fatalf("identity may use an old/readonly snapshot: %#v", connection.options)
	}
	if len(tx.statements) != 1 || tx.statements[0] != catalogLock || len(tx.queries) != 0 || tx.rollbacks != 0 {
		t.Fatalf("catalog identity read before full fence: %#v", tx)
	}
	for _, catalog := range []string{"pg_authid", "pg_auth_members", "pg_database", "pg_tablespace", "pg_shdescription", "pg_shdepend"} {
		if !strings.Contains(catalogLock, "pg_catalog."+catalog) {
			t.Fatalf("missing %s fence", catalog)
		}
	}
	if !strings.HasSuffix(catalogLock, "SHARE ROW EXCLUSIVE MODE NOWAIT") {
		t.Fatal("unbounded/unlocked catalog fallback")
	}
}

func TestBeginFailsClosedBeforeAnyIdentityRead(t *testing.T) {
	if tx, err := Begin(t.Context(), nil); err == nil || tx != nil {
		t.Fatal("missing connection accepted")
	}
	if tx, err := Begin(t.Context(), &starter{}); err == nil || tx != nil {
		t.Fatal("missing transaction accepted")
	}
	fault := errors.New("begin denied")
	if tx, err := Begin(t.Context(), &starter{err: fault}); !errors.Is(err, fault) || tx != nil {
		t.Fatal("begin failure ignored")
	}
	for _, cause := range []error{errors.New("lock privilege denied"), errors.New("catalog writer already active"), context.Canceled} {
		tx := &transaction{lockErr: cause}
		got, err := Begin(t.Context(), &starter{tx: tx})
		if got != nil || !errors.Is(err, cause) || tx.rollbacks != 1 || len(tx.queries) != 0 || len(tx.statements) != 1 {
			t.Fatalf("failed fence exposed transaction: %v %v %#v", got, err, tx)
		}
		if tx.rollbackCanceled || tx.rollbackDeadline <= 0 || tx.rollbackDeadline > 5*time.Second {
			t.Fatalf("rollback lacked fresh bounded context: %#v", tx)
		}
	}
}

func TestBeginRetainsRollbackFailureAndCallerCancellation(t *testing.T) {
	lockErr, rollbackErr := errors.New("lock denied"), errors.New("rollback failed")
	tx := &transaction{lockErr: lockErr, rollbackErr: rollbackErr}
	if got, err := Begin(t.Context(), &starter{tx: tx}); got != nil || !errors.Is(err, lockErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback uncertainty lost: %v %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := Begin(ctx, &starter{tx: tx}); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v %v", got, err)
	}
}
