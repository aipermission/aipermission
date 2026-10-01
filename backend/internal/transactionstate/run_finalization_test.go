package transactionstate

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRunAcknowledgedCommitSurvivesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	state := &runDriver{onCommit: cancel}
	database := openRunDatabase(t, state)
	err := Run(ctx, database, func(*sql.Tx) error {
		state.calls.callback++
		return nil
	})
	if err != nil || IsNotCommitted(err) || IsReadbackSafe(err) {
		t.Fatalf("acknowledged commit = %v, want success without failure markers", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("driver Commit did not cancel the caller context: %v", ctx.Err())
	}
	assertRunConnection(t, database, state, runCalls{connect: 1, begin: 1, callback: 1, commit: 1})
}

func TestRunFailedFinalizationWaitsForPhysicalClose(t *testing.T) {
	for _, name := range []string{"failed commit", "failed rollback"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New(name)
			state := &runDriver{}
			want := runCalls{connect: 1, begin: 1, callback: 1, close: 1}
			causes := []error{cause}
			if name == "failed commit" {
				state.commitErr, state.onCommit = cause, cancel
				want.commit = 1
			} else {
				state.rollbackErr = cause
				want.rollback = 1
				causes = append(causes, context.Canceled)
			}
			database := openRunDatabase(t, state)
			closeStarted, closeRelease := make(chan struct{}), make(chan struct{})
			announceClose := sync.OnceFunc(func() { close(closeStarted) })
			releaseClose := sync.OnceFunc(func() { close(closeRelease) })
			state.onClose = func() {
				announceClose()
				<-closeRelease
			}
			finished := make(chan struct{})
			var runErr error
			// Release and join before openRunDatabase's cleanup, including Fatal paths.
			t.Cleanup(func() {
				releaseClose()
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Error("Run did not finish after releasing physical Close during teardown")
				}
			})
			go func() {
				defer close(finished)
				runErr = Run(ctx, database, func(*sql.Tx) error {
					state.calls.callback++
					if name == "failed rollback" {
						cancel()
					}
					return nil
				})
			}()
			select {
			case <-closeStarted:
			case <-finished:
				t.Fatalf("Run returned without starting physical retirement: %v", runErr)
			case <-time.After(time.Second):
				t.Fatal("physical retirement did not start")
			}
			select {
			case <-finished:
				t.Fatalf("Run returned before physical Close finished: %v (safe readback=%v)", runErr, IsReadbackSafe(runErr))
			case <-time.After(20 * time.Millisecond):
			}
			releaseClose()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("Run did not finish after physical Close was released")
			}
			assertRunFailure(t, runErr, false, causes...)
			assertRunConnection(t, database, state, want)
		})
	}
}
