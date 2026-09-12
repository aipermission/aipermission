package pipedrain

import (
	"sync/atomic"
	"time"
)

// Group exposes a completion signal without adding an unbounded goroutine that
// waits on sync.WaitGroup when a transport reader fails to unblock on close.
type Group struct {
	remaining atomic.Int32
	done      chan struct{}
}

func New(hasStderr bool) *Group {
	group := &Group{done: make(chan struct{})}
	count := int32(1)
	if hasStderr {
		count++
	}
	group.remaining.Store(count)
	return group
}

func (group *Group) Done() {
	if group == nil {
		return
	}
	if group.remaining.Add(-1) == 0 {
		close(group.done)
	}
}

func (group *Group) Wait(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-group.done:
		return true
	case <-timer.C:
		return false
	}
}

// Finish invokes finish after the readers drain or the bounded wait expires.
// The timeout path deliberately does not retain another waiter; callers use it
// to wipe secrets and reject any output arriving from a broken late reader.
func (group *Group) Finish(timeout time.Duration, finish func()) {
	group.Wait(timeout)
	finish()
}
