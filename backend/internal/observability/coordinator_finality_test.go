package observability

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

func TestCoordinatorTransactionFinality(t *testing.T) {
	beginCause := errors.New("begin failed")
	callbackCause := errors.New("callback failed")
	rollbackCause := errors.New("rollback failed")
	commitCause := errors.New("commit failed")
	nestedCallback := fmt.Errorf("callback context: %w", transactionstate.NotCommitted(callbackCause))
	nestedCommit := fmt.Errorf("driver context: %w", transactionstate.NotCommitted(commitCause))

	for _, test := range []struct {
		name             string
		beginErr         error
		callbackErr      error
		rollbackErr      error
		commitErr        error
		manualCommit     bool
		wantNotCommitted bool
		wantReadbackSafe bool
		wantRetired      bool
		wantCauses       []error
		wantCalls        coordinatorFinalityCalls
	}{
		{
			name: "begin failure", beginErr: beginCause,
			wantNotCommitted: true, wantCauses: []error{beginCause},
			wantRetired: true,
			wantCalls:   coordinatorFinalityCalls{begin: 1},
		},
		{
			name: "callback failure with acknowledged rollback", callbackErr: callbackCause,
			wantNotCommitted: true, wantCauses: []error{callbackCause},
			wantCalls: coordinatorFinalityCalls{begin: 1, callback: 1, rollback: 1},
		},
		{
			name: "rollback failure", callbackErr: callbackCause, rollbackErr: rollbackCause,
			wantReadbackSafe: true, wantRetired: true,
			wantCauses: []error{callbackCause, rollbackCause},
			wantCalls:  coordinatorFinalityCalls{begin: 1, callback: 1, rollback: 1},
		},
		{
			name: "rollback failure masks nested not committed", callbackErr: nestedCallback, rollbackErr: rollbackCause,
			wantReadbackSafe: true, wantRetired: true,
			wantCauses: []error{nestedCallback, callbackCause, rollbackCause},
			wantCalls:  coordinatorFinalityCalls{begin: 1, callback: 1, rollback: 1},
		},
		{
			name: "callback commits then fails", callbackErr: callbackCause, manualCommit: true,
			wantReadbackSafe: true, wantRetired: true,
			wantCauses: []error{callbackCause, sql.ErrTxDone},
			wantCalls:  coordinatorFinalityCalls{begin: 1, callback: 1, commit: 1},
		},
		{
			name: "commit failure", commitErr: commitCause,
			wantReadbackSafe: true, wantRetired: true,
			wantCauses: []error{commitCause},
			wantCalls:  coordinatorFinalityCalls{begin: 1, callback: 1, commit: 1},
		},
		{
			name: "commit failure masks nested not committed", commitErr: nestedCommit,
			wantReadbackSafe: true, wantRetired: true,
			wantCauses: []error{nestedCommit, commitCause},
			wantCalls:  coordinatorFinalityCalls{begin: 1, callback: 1, commit: 1},
		},
		{
			name:      "successful commit",
			wantCalls: coordinatorFinalityCalls{begin: 1, callback: 1, commit: 1},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &coordinatorFinalityDriver{
				beginErr: test.beginErr, rollbackErr: test.rollbackErr, commitErr: test.commitErr,
			}
			database := openCoordinatorFinalityDatabase(t, state)
			coordinator := NewCoordinator(database, nil, nil, nil)
			err := coordinator.WithTransaction(t.Context(), func(tx *sql.Tx, appendAudit Appender) error {
				state.calls.callback++
				if tx == nil || appendAudit == nil {
					t.Fatal("callback must receive a transaction and appender")
				}
				if test.manualCommit {
					if err := tx.Commit(); err != nil {
						t.Fatalf("manual commit: %v", err)
					}
				}
				return test.callbackErr
			})
			if (err != nil) != (len(test.wantCauses) != 0) {
				t.Fatalf("WithTransaction error = %v, want causes %v", err, test.wantCauses)
			}
			if got := transactionstate.IsNotCommitted(err); got != test.wantNotCommitted {
				t.Errorf("IsNotCommitted(%v) = %v, want %v", err, got, test.wantNotCommitted)
			}
			if got := transactionstate.IsReadbackSafe(err); got != test.wantReadbackSafe {
				t.Errorf("safe readback = %v, want %v", got, test.wantReadbackSafe)
			}
			if got := state.closed == 1; got != test.wantRetired {
				t.Errorf("physical connection closes = %d, want retired=%v", state.closed, test.wantRetired)
			}
			for _, cause := range test.wantCauses {
				if !errors.Is(err, cause) {
					t.Errorf("WithTransaction error = %v, lost cause %v", err, cause)
				}
			}
			if state.calls != test.wantCalls {
				t.Errorf("calls = %+v, want %+v", state.calls, test.wantCalls)
			}
		})
	}
}

func TestCoordinatorUnavailableFinality(t *testing.T) {
	for _, name := range []string{"nil coordinator", "nil database", "nil callback"} {
		t.Run(name, func(t *testing.T) {
			state := &coordinatorFinalityDriver{}
			database := openCoordinatorFinalityDatabase(t, state)
			coordinator := NewCoordinator(database, nil, nil, nil)
			callback := func(*sql.Tx, Appender) error {
				state.calls.callback++
				return nil
			}
			switch name {
			case "nil coordinator":
				coordinator = nil
			case "nil database":
				coordinator = NewCoordinator(nil, nil, nil, nil)
			case "nil callback":
				callback = nil
			}
			err := coordinator.WithTransaction(t.Context(), callback)
			if err == nil || !transactionstate.IsNotCommitted(err) {
				t.Errorf("WithTransaction error = %v, want a not-committed failure", err)
			}
			if state.calls != (coordinatorFinalityCalls{}) {
				t.Errorf("calls = %+v, want no calls", state.calls)
			}
		})
	}
}

type coordinatorFinalityCalls struct {
	begin, callback, commit, rollback, unexpectedSQL int
}

// The fixture only supports transaction primitives; appender construction must not issue SQL.
type coordinatorFinalityDriver struct {
	beginErr, rollbackErr, commitErr error
	calls                            coordinatorFinalityCalls
	closed                           int
}

func openCoordinatorFinalityDatabase(t *testing.T, state *coordinatorFinalityDriver) *sql.DB {
	t.Helper()
	database := sql.OpenDB(state)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close finality database: %v", err)
		}
	})
	return database
}

func (d *coordinatorFinalityDriver) Connect(context.Context) (driver.Conn, error) {
	return &coordinatorFinalityConn{state: d}, nil
}

func (d *coordinatorFinalityDriver) Driver() driver.Driver { return d }

func (*coordinatorFinalityDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("unexpected driver.Open: use connector")
}

type coordinatorFinalityConn struct {
	state *coordinatorFinalityDriver
}

func (c *coordinatorFinalityConn) Prepare(query string) (driver.Stmt, error) {
	c.state.calls.unexpectedSQL++
	return nil, fmt.Errorf("unexpected SQL: %s", query)
}

func (c *coordinatorFinalityConn) Close() error {
	c.state.closed++
	return nil
}

func (c *coordinatorFinalityConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *coordinatorFinalityConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.state.calls.begin++
	if c.state.beginErr != nil {
		return nil, c.state.beginErr
	}
	return &coordinatorFinalityTx{state: c.state}, nil
}

type coordinatorFinalityTx struct {
	state *coordinatorFinalityDriver
}

func (tx *coordinatorFinalityTx) Commit() error {
	tx.state.calls.commit++
	return tx.state.commitErr
}

func (tx *coordinatorFinalityTx) Rollback() error {
	tx.state.calls.rollback++
	return tx.state.rollbackErr
}
