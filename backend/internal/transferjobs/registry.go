package transferjobs

import (
	"context"
	"sync"
)

// Registry keeps file and batch IDs in separate namespaces within one runtime.
// All zero values are usable; registry values must not be copied after use.
type Registry struct {
	Files   Group
	Batches Group
}

// Close also cancels jobs that try to register after shutdown has started.
func (r *Registry) Close() {
	r.Shutdown(context.Background())
	r.Files.clear()
	r.Batches.clear()
}

type job struct {
	cancel  context.CancelFunc
	control *Control
	running bool
}

type Group struct {
	mu      sync.Mutex
	jobs    map[int64]job
	closed  bool
	running int
	waiters []chan struct{}
}

// Launch registers a job before its goroutine starts so shutdown can reject
// late work and wait for every accepted runner to finish using runtime state.
func (g *Group) Launch(id int64, cancel context.CancelFunc, run func()) bool {
	if cancel == nil || run == nil {
		if cancel != nil {
			cancel()
		}
		return false
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		cancel()
		return false
	}
	entry := g.jobs[id]
	if entry.running {
		g.mu.Unlock()
		cancel()
		return false
	}
	entry.cancel = cancel
	entry.running = true
	g.running++
	g.set(id, entry)
	g.mu.Unlock()
	go func() {
		defer cancel()
		defer g.finish(id)
		run()
	}()
	return true
}

func (g *Group) RegisterCancel(id int64, cancel context.CancelFunc) {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	defer g.mu.Unlock()
	entry := g.jobs[id]
	entry.cancel = cancel
	g.set(id, entry)
}

func (g *Group) UnregisterCancel(id int64) { g.RegisterCancel(id, nil) }

func (g *Group) RegisterControl(id int64, control *Control) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	entry := g.jobs[id]
	entry.control = control
	g.set(id, entry)
}

func (g *Group) UnregisterControl(id int64) { g.RegisterControl(id, nil) }

// set is called only while mu is held.
func (g *Group) set(id int64, entry job) {
	if entry.cancel == nil && entry.control == nil && !entry.running {
		delete(g.jobs, id)
		return
	}
	if g.jobs == nil {
		g.jobs = map[int64]job{}
	}
	g.jobs[id] = entry
}

func (g *Group) finish(id int64) {
	g.mu.Lock()
	entry := g.jobs[id]
	if entry.running {
		entry.running = false
		entry.cancel = nil
		g.running--
		g.set(id, entry)
	}
	if g.running == 0 {
		for _, waiter := range g.waiters {
			close(waiter)
		}
		g.waiters = nil
	}
	g.mu.Unlock()
}

func (g *Group) Control(id int64) *Control {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.jobs[id].control
}

func (g *Group) Cancel(id int64) bool {
	g.mu.Lock()
	cancel := g.jobs[id].cancel
	g.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (g *Group) clear() {
	g.mu.Lock()
	g.jobs = nil
	g.mu.Unlock()
}

func (g *Group) close() {
	cancels := g.beginClose()
	for _, cancel := range cancels {
		cancel()
	}
	g.wait(context.Background())
	g.clear()
}

func (g *Group) beginClose() []context.CancelFunc {
	g.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(g.jobs))
	for _, entry := range g.jobs {
		if entry.cancel != nil {
			cancels = append(cancels, entry.cancel)
		}
	}
	g.closed = true
	g.mu.Unlock()
	return cancels
}

func (g *Group) wait(ctx context.Context) bool {
	g.mu.Lock()
	if g.running == 0 {
		g.mu.Unlock()
		return true
	}
	waiter := make(chan struct{})
	g.waiters = append(g.waiters, waiter)
	g.mu.Unlock()
	select {
	case <-waiter:
		return true
	case <-ctx.Done():
		return false
	}
}

// Shutdown cancels both job namespaces before waiting, preventing a batch
// runner from creating more file work while the file namespace is draining.
func (r *Registry) Shutdown(ctx context.Context) bool {
	cancels := append(r.Files.beginClose(), r.Batches.beginClose()...)
	for _, cancel := range cancels {
		cancel()
	}
	filesDone := r.Files.wait(ctx)
	batchesDone := r.Batches.wait(ctx)
	return filesDone && batchesDone
}

// Wait blocks until all runners accepted by Launch have returned.
func (r *Registry) Wait(ctx context.Context) bool {
	filesDone := r.Files.wait(ctx)
	batchesDone := r.Batches.wait(ctx)
	return filesDone && batchesDone
}
