package gatewayworkspace

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/databaseownership"
	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
)

type shutdownBoundary struct {
	t       *testing.T
	db      *sql.DB
	mu      sync.Mutex
	events  map[string][]string
	begun   chan string
	waiting chan string
	release <-chan struct{}
}

func (boundary *shutdownBoundary) record(owner, stage string) {
	boundary.t.Helper()
	if err := boundary.db.Ping(); err != nil {
		boundary.t.Errorf("storage closed during %s/%s: %v", owner, stage, err)
	}
	boundary.mu.Lock()
	boundary.events[owner] = append(boundary.events[owner], stage)
	boundary.mu.Unlock()
}

func (boundary *shutdownBoundary) begin(owner string) {
	boundary.record(owner, "begin")
	boundary.begun <- owner
}

func (boundary *shutdownBoundary) wait(ctx context.Context, owner string) error {
	boundary.record(owner, "wait")
	if boundary.waiting != nil {
		boundary.waiting <- owner
	}
	select {
	case <-boundary.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type boundaryActions struct{ *shutdownBoundary }

func (workflow boundaryActions) BeginShutdown() { workflow.begin("actions") }
func (workflow boundaryActions) WaitShutdown(ctx context.Context) error {
	return workflow.wait(ctx, "actions")
}
func (workflow boundaryActions) MarkRunningOutcomeUnknown(ctx context.Context, reason string) error {
	workflow.record("actions", "recover")
	if ctx.Err() != nil || reason != runtimeoutcome.ConnectorActionUnknown {
		workflow.t.Errorf("action recovery context/reason: %v %q", ctx.Err(), reason)
	}
	return nil
}

type boundaryCommands struct{ *shutdownBoundary }

func (workflow boundaryCommands) BeginWorkerShutdown() { workflow.begin("commands") }
func (workflow boundaryCommands) WaitWorkers(ctx context.Context) error {
	return workflow.wait(ctx, "commands")
}
func (workflow boundaryCommands) CancelRunning(ctx context.Context, reason string) error {
	workflow.record("commands", "recover")
	if ctx.Err() != nil || reason != runtimeoutcome.CommandCanceled {
		workflow.t.Errorf("command recovery context/reason: %v %q", ctx.Err(), reason)
	}
	return nil
}

type boundaryTransfers struct{ *shutdownBoundary }

func (workflow boundaryTransfers) BeginShutdown() (bool, error) {
	workflow.begin("transfers")
	return true, nil
}
func (workflow boundaryTransfers) Wait(ctx context.Context) bool {
	return workflow.wait(ctx, "transfers") == nil
}
func (workflow boundaryTransfers) Recover(ctx context.Context, active, queued string) error {
	workflow.record("transfers", "recover")
	if ctx.Err() != nil || active != "interrupted by workspace shutdown" || queued != "queue stopped by workspace shutdown" {
		workflow.t.Errorf("transfer recovery context/reasons: %v %q %q", ctx.Err(), active, queued)
	}
	return nil
}
func (workflow boundaryTransfers) Abort(ctx context.Context) bool {
	return workflow.wait(ctx, "abort") == nil
}

func TestWorkspaceCloseBridgesWorkersBeforeNativeStorageRelease(t *testing.T) {
	input := nativeWorkspaceInput(t, "closing")
	runtime := openNativeWorkspace(t, input)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	boundary := &shutdownBoundary{t: t, db: runtime.WorkspaceDatabase(), events: make(map[string][]string), begun: make(chan string, 3), waiting: make(chan string, 3), release: release}
	result := make(chan error, 1)
	done := make(chan struct{})
	var completed atomic.Int64
	earlyFailure := os.Getenv(shutdownFailureEnvironment) == "1"
	// Registered after native storage cleanup so no fixture closes under a worker.
	t.Cleanup(func() {
		unblock()
		<-done
		if err := runtime.WaitTeardown(context.Background()); err != nil {
			t.Errorf("join shutdown fixture: %v", err)
		}
		if earlyFailure {
			t.Log("shutdown fixture joined before native storage cleanup")
		}
	})
	go func() {
		defer close(done)
		result <- (&Component{}).Close(runtime,
			func() (ActionWorkflow, error) { return boundaryActions{boundary}, nil },
			func() (CommandWorkflow, error) { return boundaryCommands{boundary}, nil },
			func() TransferWorkflow { return boundaryTransfers{boundary} },
			func() {
				if boundary.db.Ping() == nil {
					t.Error("completion ran before native storage closed")
				}
				completed.Add(1)
			},
		)
	}()
	for count := 0; count < 3; count++ {
		select {
		case <-boundary.begun:
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown did not begin all workers before waiting")
		}
	}
	for count := 0; count < 3; count++ {
		select {
		case <-boundary.waiting:
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown workers did not reach the wait barrier")
		}
	}
	if earlyFailure {
		t.Fatal("intentional shutdown fixture failure")
	}
	if err := boundary.db.Ping(); err != nil {
		t.Fatalf("blocked workers lost native storage: %v", err)
	}
	if _, err := runtime.TagActionIdentity([]byte("request")); err != nil {
		t.Fatal("action identity was cleared before worker recovery")
	}
	if completed.Load() != 0 {
		t.Fatal("close completed while workers still own storage")
	}
	unblock()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish after worker release")
	}
	if completed.Load() != 1 || boundary.db.Ping() == nil {
		t.Fatal("completion did not follow storage close exactly once")
	}
	boundary.mu.Lock()
	defer boundary.mu.Unlock()
	for _, owner := range []string{"actions", "commands", "transfers"} {
		if !reflect.DeepEqual(boundary.events[owner], []string{"begin", "wait", "recover"}) {
			t.Errorf("%s lifecycle = %v", owner, boundary.events[owner])
		}
	}
	ownership, err := databaseownership.Acquire(input.Path)
	if err != nil {
		t.Fatalf("completed close retained ownership: %v", err)
	}
	if err := ownership.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceDiscardAbortsWithoutNormalRecovery(t *testing.T) {
	runtime := openNativeWorkspace(t, nativeWorkspaceInput(t, "discarded"))
	release := make(chan struct{})
	close(release)
	boundary := &shutdownBoundary{t: t, db: runtime.WorkspaceDatabase(), events: make(map[string][]string), release: release}
	completed := 0
	if err := (&Component{}).Discard(runtime, func() TransferWorkflow { return boundaryTransfers{boundary} }, func() {
		if boundary.db.Ping() == nil {
			t.Error("discard completion preceded storage close")
		}
		completed++
	}); err != nil {
		t.Fatal(err)
	}
	if completed != 1 || boundary.db.Ping() == nil || len(boundary.events) != 1 || !reflect.DeepEqual(boundary.events["abort"], []string{"wait"}) {
		t.Fatalf("discard ran normal recovery or failed storage release: events=%v completed=%d", boundary.events, completed)
	}
}
