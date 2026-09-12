package backup

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type lifecycleStub struct {
	acquire func(context.Context) (func(), error)
}

func (l lifecycleStub) AcquireReadContext(ctx context.Context) (func(), error) {
	return l.acquire(ctx)
}

func (lifecycleStub) Import(context.Context, workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
	panic("not used")
}

func TestAcquireReadOperationUsesBackupSlotBeforeLifecycle(t *testing.T) {
	events := []string{}
	component := New(Dependencies{
		Lifecycle: lifecycleStub{acquire: func(context.Context) (func(), error) {
			events = append(events, "lifecycle.acquire")
			return func() { events = append(events, "lifecycle.release") }, nil
		}},
		AcquireOperation: func(context.Context) (func(), error) {
			events = append(events, "operation.acquire")
			return func() { events = append(events, "operation.release") }, nil
		},
	})
	lease, err := component.acquireReadOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease.ReleaseLifecycle()
	lease.Release()
	want := []string{"operation.acquire", "lifecycle.acquire", "lifecycle.release", "operation.release"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lease order = %v, want %v", events, want)
	}
}

func TestAcquireReadOperationReleasesSlotWhenLifecycleAdmissionFails(t *testing.T) {
	wantErr := errors.New("workspace is locking")
	released := false
	component := New(Dependencies{
		Lifecycle: lifecycleStub{acquire: func(context.Context) (func(), error) {
			return nil, wantErr
		}},
		AcquireOperation: func(context.Context) (func(), error) {
			return func() { released = true }, nil
		},
	})
	if _, err := component.acquireReadOperation(t.Context()); !errors.Is(err, wantErr) {
		t.Fatalf("acquireReadOperation() error = %v, want %v", err, wantErr)
	}
	if !released {
		t.Fatal("operation slot was retained after lifecycle admission failed")
	}
}

func TestContendedBackupSlotDoesNotBlockExclusiveLifecycleTransition(t *testing.T) {
	var lifecycleMu sync.RWMutex
	var readAcquires atomic.Int32
	var operationAcquires atomic.Int32
	thirdAttempted := make(chan struct{})
	limiter := &OperationLimiter{}
	component := New(Dependencies{
		Lifecycle: lifecycleStub{acquire: func(context.Context) (func(), error) {
			lifecycleMu.RLock()
			readAcquires.Add(1)
			return lifecycleMu.RUnlock, nil
		}},
		AcquireOperation: func(ctx context.Context) (func(), error) {
			if operationAcquires.Add(1) == 3 {
				close(thirdAttempted)
			}
			return limiter.Acquire(ctx)
		},
	})
	first, err := component.acquireReadOperation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := component.acquireReadOperation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()

	thirdResult := make(chan *readOperationLease, 1)
	thirdError := make(chan error, 1)
	go func() {
		lease, acquireErr := component.acquireReadOperation(t.Context())
		if acquireErr != nil {
			thirdError <- acquireErr
			return
		}
		thirdResult <- lease
	}()
	select {
	case <-thirdAttempted:
	case <-time.After(time.Second):
		t.Fatal("third operation did not reach the contended slot")
	}
	if got := readAcquires.Load(); got != 2 {
		t.Fatalf("contended operation acquired lifecycle read lock %d times, want 2", got)
	}

	first.ReleaseLifecycle()
	second.ReleaseLifecycle()
	exclusive := make(chan struct{})
	go func() {
		lifecycleMu.Lock()
		close(exclusive)
		lifecycleMu.Unlock()
	}()
	select {
	case <-exclusive:
	case <-time.After(time.Second):
		t.Fatal("exclusive lifecycle transition was blocked by a request waiting for an operation slot")
	}

	first.Release()
	select {
	case lease := <-thirdResult:
		lease.Release()
	case err := <-thirdError:
		t.Fatalf("third operation failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("third operation did not acquire the released slot")
	}
}
