package backup

import (
	"context"
	"reflect"
	"testing"

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

func TestAcquireReadOperationUsesLifecycleBeforeBackupSlot(t *testing.T) {
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
	want := []string{"lifecycle.acquire", "operation.acquire", "lifecycle.release", "operation.release"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lease order = %v, want %v", events, want)
	}
}
