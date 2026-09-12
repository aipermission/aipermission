package workspacelifecycle

import (
	"context"
	"sync"
)

// requestGate is a context-aware writer-preferring read/write gate. Lifecycle
// mutations must not wait forever behind requests whose contexts have expired.
type requestGate struct {
	mu             sync.Mutex
	changed        chan struct{}
	readers        int
	writer         bool
	waitingWriters int
}

func newRequestGate() *requestGate {
	return &requestGate{changed: make(chan struct{})}
}

func (gate *requestGate) acquireRead(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		gate.mu.Lock()
		if !gate.writer && gate.waitingWriters == 0 {
			gate.readers++
			gate.mu.Unlock()
			return gate.releaseRead, nil
		}
		changed := gate.changed
		gate.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (gate *requestGate) acquireMutation(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate.mu.Lock()
	gate.waitingWriters++
	gate.notifyLocked()
	gate.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			gate.mu.Lock()
			gate.waitingWriters--
			gate.notifyLocked()
			gate.mu.Unlock()
			return nil, err
		}
		gate.mu.Lock()
		if !gate.writer && gate.readers == 0 {
			gate.waitingWriters--
			gate.writer = true
			gate.mu.Unlock()
			return gate.releaseMutation, nil
		}
		changed := gate.changed
		gate.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			gate.mu.Lock()
			gate.waitingWriters--
			gate.notifyLocked()
			gate.mu.Unlock()
			return nil, ctx.Err()
		}
	}
}

func (gate *requestGate) releaseRead() {
	gate.mu.Lock()
	if gate.readers > 0 {
		gate.readers--
		gate.notifyLocked()
	}
	gate.mu.Unlock()
}

func (gate *requestGate) releaseMutation() {
	gate.mu.Lock()
	if gate.writer {
		gate.writer = false
		gate.notifyLocked()
	}
	gate.mu.Unlock()
}

func (gate *requestGate) notifyLocked() {
	close(gate.changed)
	gate.changed = make(chan struct{})
}
