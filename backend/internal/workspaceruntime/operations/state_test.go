package operations

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func TestActionWorkflowOrCreatePublishesOneRuntime(t *testing.T) {
	state := &State{}
	var calls atomic.Int64
	const workers = 20
	results := make(chan *actions.Runtime, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			owner, err := state.ActionWorkflowOrCreate(func() (*actions.Runtime, error) {
				calls.Add(1)
				return &actions.Runtime{}, nil
			})
			if err != nil {
				t.Errorf("create action workflow: %v", err)
				return
			}
			results <- owner
		}()
	}
	group.Wait()
	close(results)
	var first *actions.Runtime
	for result := range results {
		if first == nil {
			first = result
		}
		if result != first {
			t.Fatal("callers observed different action runtimes")
		}
	}
	if calls.Load() != 1 || state.ActionWorkflow() != first {
		t.Fatalf("factory calls=%d cached=%p want=%p", calls.Load(), state.ActionWorkflow(), first)
	}
}

func TestProjectVaultOrCreatePublishesOneRuntime(t *testing.T) {
	state := &State{}
	var calls atomic.Int64
	const workers = 20
	results := make(chan *projectvault.Runtime, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			owner, err := state.ProjectVaultOrCreate(func() (*projectvault.Runtime, error) {
				calls.Add(1)
				return &projectvault.Runtime{}, nil
			})
			if err != nil {
				t.Errorf("create project Vault runtime: %v", err)
				return
			}
			results <- owner
		}()
	}
	group.Wait()
	close(results)
	var first *projectvault.Runtime
	for result := range results {
		if first == nil {
			first = result
		}
		if result != first {
			t.Fatal("callers observed different project Vault runtimes")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("factory calls=%d", calls.Load())
	}
}
