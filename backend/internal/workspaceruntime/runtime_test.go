package workspaceruntime

import (
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
