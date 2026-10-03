package persistence

import (
	"context"
	"sync"
)

// OutputGate preserves snapshot/store/live ordering without trapping a command
// observer behind a producer blocked on transcript storage. Its zero value is ready.
type OutputGate struct {
	once  sync.Once
	token chan struct{}
}

func (gate *OutputGate) initialize() {
	gate.once.Do(func() {
		gate.token = make(chan struct{}, 1)
		gate.token <- struct{}{}
	})
}

func (gate *OutputGate) Lock(ctx context.Context) error {
	if gate.TryLock() {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-gate.token:
		return nil
	}
}

func (gate *OutputGate) TryLock() bool {
	gate.initialize()
	select {
	case <-gate.token:
		return true
	default:
		return false
	}
}

func (gate *OutputGate) Unlock() { gate.token <- struct{}{} }
