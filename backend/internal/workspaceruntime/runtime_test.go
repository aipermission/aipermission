package workspaceruntime

import (
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
)

func TestEnsureIdentityInitializesEachIdentityOnce(t *testing.T) {
	runtime := &Runtime{}
	var workspaceCalls atomic.Int64
	var instanceCalls atomic.Int64
	const workers = 20
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			if err := runtime.EnsureIdentity(
				func(*sql.DB) (string, error) {
					workspaceCalls.Add(1)
					return "workspace-id", nil
				},
				func() (string, error) {
					instanceCalls.Add(1)
					return "runtime-id", nil
				},
			); err != nil {
				t.Errorf("ensure identity: %v", err)
			}
		}()
	}
	group.Wait()
	if runtime.WorkspaceUUID != "workspace-id" || runtime.RuntimeInstanceID != "runtime-id" {
		t.Fatalf("identity = %q/%q", runtime.WorkspaceUUID, runtime.RuntimeInstanceID)
	}
	if workspaceCalls.Load() != 1 || instanceCalls.Load() != 1 {
		t.Fatalf("initializers called workspace=%d runtime=%d", workspaceCalls.Load(), instanceCalls.Load())
	}
}
