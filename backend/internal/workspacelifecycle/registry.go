package workspacelifecycle

import "sync"

// Identity is the stable workspace metadata needed by lifecycle and transport
// code without exposing the runtime implementation.
type Identity struct {
	ID            string
	Path          string
	RetryIdentity string
}

// Describe extracts lifecycle identity from a runtime owned by the composition
// root.
type Describe[T any] func(T) Identity

// Registry is the single source of truth for unlocked runtimes and the active
// workspace selection. Lifecycle mutations are serialized by Service; the
// registry mutex also protects readers that outlive an HTTP request.
type Registry[T any] struct {
	mu          sync.RWMutex
	defaultPath string
	active      Identity
	runtimes    map[string]T
	describe    Describe[T]
}

func NewRegistry[T any](defaultPath, defaultID string, describe Describe[T]) *Registry[T] {
	return &Registry[T]{
		defaultPath: defaultPath,
		active:      Identity{ID: defaultID, Path: defaultPath},
		runtimes:    make(map[string]T),
		describe:    describe,
	}
}

func (r *Registry[T]) IsUnlocked() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.runtimes) > 0
}

func (r *Registry[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.runtimes)
}

func (r *Registry[T]) Selection() Identity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

func (r *Registry[T]) Select(identity Identity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = identity
}

func (r *Registry[T]) Active() (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runtime, ok := r.runtimes[r.active.ID]
	return runtime, ok
}

func (r *Registry[T]) Lookup(id string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runtime, ok := r.runtimes[id]
	return runtime, ok
}

func (r *Registry[T]) Activate(runtime T) Identity {
	identity := r.describe(runtime)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[identity.ID] = runtime
	r.active = identity
	return identity
}

func (r *Registry[T]) Remove(id string, promote bool) (removed T, found bool, promoted T, promotedOK bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed, found = r.runtimes[id]
	delete(r.runtimes, id)
	if r.active.ID == id {
		r.active = Identity{ID: id, Path: r.active.Path}
	}
	if promote {
		for _, candidate := range r.runtimes {
			identity := r.describe(candidate)
			r.active = identity
			return removed, found, candidate, true
		}
	}
	return removed, found, promoted, false
}

func (r *Registry[T]) ResetSelection(defaultID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = Identity{ID: defaultID, Path: r.defaultPath}
}

func (r *Registry[T]) Snapshot() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]T, 0, len(r.runtimes))
	seenActive := false
	if runtime, ok := r.runtimes[r.active.ID]; ok {
		result = append(result, runtime)
		seenActive = true
	}
	for id, runtime := range r.runtimes {
		if seenActive && id == r.active.ID {
			continue
		}
		result = append(result, runtime)
	}
	return result
}

func (r *Registry[T]) Clear(defaultID string) []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]T, 0, len(r.runtimes))
	for _, runtime := range r.runtimes {
		result = append(result, runtime)
	}
	r.runtimes = make(map[string]T)
	r.active = Identity{ID: defaultID, Path: r.defaultPath}
	return result
}
