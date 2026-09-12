package workspaceruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeStoresCompositionStateWithoutExportingServiceLocatorMethods(t *testing.T) {
	runtime := &Runtime{WorkspaceUUID: "workspace-id", RuntimeInstanceID: "runtime-id"}
	if runtime.WorkspaceUUID != "workspace-id" || runtime.RuntimeInstanceID != "runtime-id" {
		t.Fatal("runtime identity state was not retained")
	}
}

func TestStartTeardownSharesOneCompletionSignal(t *testing.T) {
	runtime := &Runtime{}
	release := make(chan struct{})
	var runs atomic.Int32
	first := runtime.StartTeardown(func() {
		runs.Add(1)
		<-release
	})
	second := runtime.StartTeardown(func() { runs.Add(1) })
	if first != second {
		t.Fatal("repeated teardown did not return the same completion signal")
	}
	close(release)
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("teardown completion was not signaled")
	}
	if runs.Load() != 1 {
		t.Fatalf("teardown coordinator runs = %d, want 1", runs.Load())
	}
}

func TestWaitTeardownObservesDeferredOwnerCompletion(t *testing.T) {
	runtime := &Runtime{}
	release := make(chan struct{})
	runtime.StartTeardown(func() { <-release })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := runtime.WaitTeardown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitTeardown() error = %v, want deadline", err)
	}
	close(release)
	if err := runtime.WaitTeardown(t.Context()); err != nil {
		t.Fatalf("WaitTeardown() after completion: %v", err)
	}
}
