package pipedrain

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// Read copies each transport read into an immutable chunk and clears the
// reusable plaintext buffer before reading again.
func Read(ctx context.Context, reader io.Reader) <-chan string {
	chunks := make(chan string)
	go func() {
		defer close(chunks)
		buffer := make([]byte, 4096)
		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				chunk := string(buffer[:n])
				clear(buffer[:n])
				select {
				case chunks <- chunk:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return chunks
}

// Consume stops owning consume as soon as the context is canceled, even when
// a broken transport reader remains blocked.
func Consume(ctx context.Context, chunks <-chan string, consume func(string)) {
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return
			}
			consume(chunk)
		case <-ctx.Done():
			return
		}
	}
}

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
