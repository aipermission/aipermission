package rolecatalog

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type sharedLockTx struct {
	pgx.Tx
	statements []string
	failAt     int
	remaining  time.Duration
}

func (tx *sharedLockTx) Exec(ctx context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	deadline, ok := ctx.Deadline()
	if ok {
		tx.remaining = time.Until(deadline)
	}
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}
	tx.statements = append(tx.statements, statement)
	if len(tx.statements) == tx.failAt {
		return pgconn.CommandTag{}, errors.New("shared writer lock failed")
	}
	return pgconn.CommandTag{}, nil
}

func TestSharedOwnershipDrainFullyScansBeforeFreshScopeRead(t *testing.T) {
	tx := &sharedLockTx{}
	if err := lockSharedOwnershipRows(t.Context(), tx); err != nil || !reflect.DeepEqual(tx.statements, sharedOwnershipLocks[:]) {
		t.Fatalf("shared row drain=%#v %v", tx.statements, err)
	}
	if tx.remaining <= 0 || tx.remaining > 5*time.Second {
		t.Fatal("row drain lacks an overall bounded deadline")
	}
	for _, failAt := range []int{1, 2, 3} {
		tx := &sharedLockTx{failAt: failAt}
		if err := lockSharedOwnershipRows(t.Context(), tx); err == nil || len(tx.statements) != failAt {
			t.Fatal("partial catalog drain continued without all row locks")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := lockSharedOwnershipRows(ctx, &sharedLockTx{}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled drain continued")
	}
}
