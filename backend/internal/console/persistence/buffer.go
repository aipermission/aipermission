package persistence

import (
	"context"
	"sync"
	"time"
)

const MaxPendingBytes = 512 << 10

// Buffer retains accepted redacted bytes until a successful durable write.
// In-flight bytes still count against capacity; failures never reinsert copies.
type Buffer struct {
	mu       sync.Mutex
	pending  string
	changed  chan struct{}
	draining bool
	worker   bool
}

func (buffer *Buffer) signalLocked() {
	if buffer.changed != nil {
		close(buffer.changed)
	}
	buffer.changed = make(chan struct{})
}

func (buffer *Buffer) Append(ctx context.Context, data string, requestFlush func()) error {
	for data != "" {
		buffer.mu.Lock()
		if buffer.changed == nil {
			buffer.changed = make(chan struct{})
		}
		room := MaxPendingBytes - len(buffer.pending)
		if room > 0 {
			size := min(room, len(data))
			buffer.pending += data[:size]
			data = data[size:]
			buffer.mu.Unlock()
			continue
		}
		changed := buffer.changed
		buffer.mu.Unlock()
		if requestFlush != nil {
			requestFlush()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
	return nil
}

func (buffer *Buffer) Len() int {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return len(buffer.pending)
}

func (buffer *Buffer) Drain(ctx context.Context, persist func(string) error) error {
	buffer.mu.Lock()
	for buffer.draining {
		changed := buffer.changed
		buffer.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
		buffer.mu.Lock()
	}
	if err := ctx.Err(); err != nil {
		buffer.mu.Unlock()
		return err
	}
	buffer.draining = true
	buffer.signalLocked()
	pending := buffer.pending
	buffer.mu.Unlock()
	err := persist(pending)
	buffer.mu.Lock()
	if err == nil {
		buffer.pending = buffer.pending[len(pending):]
	}
	buffer.draining = false
	buffer.signalLocked()
	buffer.mu.Unlock()
	return err
}

func (buffer *Buffer) StartWorker() bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.worker {
		return false
	}
	buffer.worker = true
	return true
}

func (buffer *Buffer) FinishWorker(force bool) bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if force || buffer.pending == "" {
		buffer.worker = false
		return true
	}
	return false
}

// RunWorker retries without requiring another producer append. The caller owns
// admission and joins this worker; a missing lifetime permits one attempt only.
func (buffer *Buffer) RunWorker(ctx context.Context, interval time.Duration, flush func() error) {
	detached := ctx == nil
	if detached {
		ctx = context.Background()
	}
	delay := interval
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			buffer.FinishWorker(true)
			return
		case <-timer.C:
		}
		err := flush()
		if buffer.FinishWorker(detached || ctx.Err() != nil) {
			return
		}
		delay = 0
		if err != nil {
			delay = interval
		}
	}
}
