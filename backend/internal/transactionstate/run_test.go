package transactionstate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRunPreconditions(t *testing.T) {
	for _, name := range []string{"nil context", "nil database", "nil mutate"} {
		t.Run(name, func(t *testing.T) {
			state := &runDriver{}
			database := openRunDatabase(t, state)
			ctx, db := t.Context(), database
			mutate := func(*sql.Tx) error { state.calls.callback++; return nil }
			switch name {
			case "nil context":
				ctx = nil
			case "nil database":
				db = nil
			case "nil mutate":
				mutate = nil
			}
			assertRunFailure(t, Run(ctx, db, mutate), true)
			assertRunConnection(t, database, state, runCalls{})
		})
	}
}

func TestRunAcquireFailure(t *testing.T) {
	cause := errors.New("connect failed")
	state := &runDriver{connectErr: cause}
	database := openRunDatabase(t, state)
	err := Run(t.Context(), database, func(*sql.Tx) error {
		state.calls.callback++
		return nil
	})
	assertRunFailure(t, err, true, cause)
	assertRunConnection(t, database, state, runCalls{connect: 1})
}

func TestRunBeginFailureRetiresPhysicalConnection(t *testing.T) {
	cause := errors.New("begin failed")
	state := &runDriver{beginErr: cause}
	database := openRunDatabase(t, state)
	err := Run(t.Context(), database, func(*sql.Tx) error {
		state.calls.callback++
		return nil
	})
	assertRunFailure(t, err, true, cause)
	assertRunConnection(t, database, state, runCalls{connect: 1, begin: 1, close: 1})
}

func TestRunCancellationAfterCheckoutRejectsBegin(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	state := &runDriver{onConnect: cancel}
	database := openRunDatabase(t, state)
	err := Run(ctx, database, func(*sql.Tx) error {
		state.calls.callback++
		return nil
	})
	assertRunFailure(t, err, true, context.Canceled)
	assertRunConnection(t, database, state, runCalls{connect: 1})
}

func TestRunAcknowledgedRollbackOverridesNestedMarker(t *testing.T) {
	cause := errors.New("mutate failed")
	nested := fmt.Errorf("mutate: %w", UnknownWithSafeReadback(cause))
	state := &runDriver{}
	database := openRunDatabase(t, state)
	err := Run(t.Context(), database, func(*sql.Tx) error {
		state.calls.callback++
		return nested
	})
	assertRunFailure(t, err, true, nested, cause)
	assertRunConnection(t, database, state, runCalls{connect: 1, begin: 1, callback: 1, rollback: 1})
}

func TestRunUncertainFinalityMasksNestedMarkerAndRetiresConnection(t *testing.T) {
	callbackCause := errors.New("mutate failed")
	rollbackCause := errors.New("rollback failed")
	commitCause := errors.New("commit failed")
	nestedCallback := fmt.Errorf("mutate: %w", NotCommitted(callbackCause))
	nestedCommit := fmt.Errorf("driver: %w", NotCommitted(commitCause))
	for _, name := range []string{"failed rollback", "manual commit then error", "failed commit"} {
		t.Run(name, func(t *testing.T) {
			state := &runDriver{}
			want := runCalls{connect: 1, begin: 1, callback: 1, close: 1}
			causes := []error{nestedCallback, callbackCause}
			if name == "failed rollback" {
				state.rollbackErr = rollbackCause
				want.rollback = 1
				causes = append(causes, rollbackCause)
			} else {
				want.commit = 1
				if name == "failed commit" {
					state.commitErr = nestedCommit
					causes = []error{nestedCommit, commitCause}
				} else {
					causes = append(causes, sql.ErrTxDone)
				}
			}
			database := openRunDatabase(t, state)
			err := Run(t.Context(), database, func(tx *sql.Tx) error {
				state.calls.callback++
				if name == "manual commit then error" {
					if err := tx.Commit(); err != nil {
						t.Fatalf("manual commit: %v", err)
					}
				}
				if name == "failed commit" {
					return nil
				}
				return nestedCallback
			})
			assertRunFailure(t, err, false, causes...)
			assertRunConnection(t, database, state, want)
		})
	}
}

func TestRunSuccessfulCommitReleasesPinnedConnection(t *testing.T) {
	state := &runDriver{}
	database := openRunDatabase(t, state)
	err := Run(t.Context(), database, func(tx *sql.Tx) error {
		state.calls.callback++
		if tx == nil {
			t.Fatal("mutate received a nil transaction")
		}
		return nil
	})
	if err != nil || IsNotCommitted(err) || IsReadbackSafe(err) {
		t.Fatalf("successful Run = %v, want nil without failure markers", err)
	}
	assertRunConnection(t, database, state, runCalls{connect: 1, begin: 1, callback: 1, commit: 1})
}

func TestRunCancellationDuringMutationRetiresConnection(t *testing.T) {
	for _, name := range []string{"nil callback error", "SQL cancellation", "failed rollback"} {
		t.Run(name, func(t *testing.T) {
			state := &runDriver{}
			want := runCalls{connect: 1, begin: 1, callback: 1, rollback: 1}
			if name == "failed rollback" {
				state.rollbackErr = errors.New("rollback failed")
				want.close = 1
			}
			database := openRunDatabase(t, state)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := Run(ctx, database, func(tx *sql.Tx) error {
				state.calls.callback++
				cancel()
				if name == "SQL cancellation" {
					_, err := tx.ExecContext(ctx, "must not dispatch")
					return err
				}
				return nil
			})
			causes := []error{context.Canceled}
			if state.rollbackErr != nil {
				causes = append(causes, state.rollbackErr)
			}
			assertRunFailure(t, err, state.rollbackErr == nil, causes...)
			assertRunConnection(t, database, state, want)
		})
	}
}

func TestRunCancellationDuringBeginRejectsCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	state := &runDriver{onBegin: cancel}
	database := openRunDatabase(t, state)
	err := Run(ctx, database, func(*sql.Tx) error {
		state.calls.callback++
		return nil
	})
	assertRunFailure(t, err, true, context.Canceled)
	assertRunConnection(t, database, state, runCalls{connect: 1, begin: 1, rollback: 1})
}

func TestRunPanicRollsBackAndRepanicsUnchanged(t *testing.T) {
	for _, rollbackFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback failure=%v", rollbackFails), func(t *testing.T) {
			state := &runDriver{}
			want := runCalls{connect: 1, begin: 1, callback: 1, rollback: 1}
			if rollbackFails {
				state.rollbackErr = errors.New("rollback failed")
				want.close = 1
			}
			database := openRunDatabase(t, state)
			panicValue := &struct{ message string }{"original panic"}
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_ = Run(t.Context(), database, func(*sql.Tx) error {
					state.calls.callback++
					panic(panicValue)
				})
			}()
			if recovered != panicValue {
				t.Fatalf("recovered = %v, want original panic %v", recovered, panicValue)
			}
			assertRunConnection(t, database, state, want)
		})
	}
}

func assertRunFailure(t *testing.T, err error, notCommitted bool, causes ...error) {
	t.Helper()
	if err == nil || IsNotCommitted(err) != notCommitted || IsReadbackSafe(err) != !notCommitted {
		t.Fatalf("Run = %v, not committed=%v, safe readback=%v; want %v, %v",
			err, IsNotCommitted(err), IsReadbackSafe(err), notCommitted, !notCommitted)
	}
	for _, cause := range causes {
		if !errors.Is(err, cause) {
			t.Errorf("Run = %v, lost cause %v", err, cause)
		}
	}
}

func assertRunConnection(t *testing.T, database *sql.DB, state *runDriver, want runCalls) {
	t.Helper()
	// Check before another checkout or DB cleanup can close the original source.
	if state.calls != want {
		t.Fatalf("driver calls on return = %+v, want %+v", state.calls, want)
	}
	physical := state.physical
	if want.close != 0 && (physical == nil || !physical.closed) {
		t.Fatal("Run returned before physically closing its uncertain connection")
	}
	reuse := physical != nil && want.close == 0
	wantOpen := 0
	if reuse {
		wantOpen = 1
	}
	if stats := database.Stats(); stats.InUse != 0 || stats.OpenConnections != wantOpen {
		t.Fatalf("pool on return = %+v, want no pinned slot and %d open connections", stats, wantOpen)
	}
	state.connectErr = nil
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("bounded checkout after Run (MaxOpenConns=1): %v", err)
	}
	defer connection.Close()
	if err := connection.Raw(func(raw any) error {
		next := raw.(*runConn)
		if next.closed || (next == physical) != reuse {
			t.Errorf("readback source: closed=%v, reused=%v, want reused=%v", next.closed, next == physical, reuse)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect readback source: %v", err)
	}
	if !reuse {
		want.connect++
	}
	if state.calls != want {
		t.Errorf("driver calls after checkout = %+v, want %+v", state.calls, want)
	}
}

type runCalls struct {
	connect, begin, callback, commit, rollback, close, unexpectedSQL int
}

// Only transaction primitives are supported; no SQL engine or registration.
type runDriver struct {
	connectErr, beginErr, commitErr, rollbackErr error
	onConnect                                    func()
	onBegin                                      func()
	onCommit                                     func()
	onClose                                      func()
	calls                                        runCalls
	physical                                     *runConn
}

func openRunDatabase(t *testing.T, state *runDriver) *sql.DB {
	t.Helper()
	database := sql.OpenDB(state)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close fixture database: %v", err)
		}
	})
	return database
}

func (d *runDriver) Connect(context.Context) (driver.Conn, error) {
	d.calls.connect++
	if d.connectErr != nil {
		return nil, d.connectErr
	}
	d.physical = &runConn{state: d}
	if d.onConnect != nil {
		d.onConnect()
	}
	return d.physical, nil
}

func (d *runDriver) Driver() driver.Driver { return d }

func (*runDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("unexpected driver.Open: use connector")
}

type runConn struct {
	state  *runDriver
	closed bool
}

func (c *runConn) Prepare(query string) (driver.Stmt, error) {
	c.state.calls.unexpectedSQL++
	return nil, fmt.Errorf("unexpected SQL: %s", query)
}

func (c *runConn) Close() error {
	c.state.calls.close++
	if c.state.onClose != nil {
		c.state.onClose()
	}
	c.closed = true
	return nil
}

func (c *runConn) Begin() (driver.Tx, error) {
	c.state.calls.begin++
	if c.state.onBegin != nil {
		c.state.onBegin()
	}
	if c.state.beginErr != nil {
		return nil, c.state.beginErr
	}
	return &runTx{state: c.state}, nil
}

type runTx struct{ state *runDriver }

func (tx *runTx) Commit() error {
	tx.state.calls.commit++
	if tx.state.onCommit != nil {
		tx.state.onCommit()
	}
	return tx.state.commitErr
}

func (tx *runTx) Rollback() error {
	tx.state.calls.rollback++
	return tx.state.rollbackErr
}
