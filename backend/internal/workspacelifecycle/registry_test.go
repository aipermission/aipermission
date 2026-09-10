package workspacelifecycle

import (
	"sync"
	"testing"
)

type testRuntime struct {
	id, path, retry string
}

func describeTestRuntime(runtime *testRuntime) Identity {
	return Identity{ID: runtime.id, Path: runtime.path, RetryIdentity: runtime.retry}
}

func TestRegistryTracksOneActiveRuntimeWithoutDuplicatedState(t *testing.T) {
	registry := NewRegistry("/data/default.db", "default", describeTestRuntime)
	first := &testRuntime{id: "first", path: "/data/first.db", retry: "retry-1"}
	second := &testRuntime{id: "second", path: "/data/second.db", retry: "retry-2"}

	registry.Activate(first)
	registry.Activate(second)
	active, ok := registry.Active()
	if !ok || active != second || registry.Selection() != describeTestRuntime(second) {
		t.Fatalf("unexpected active runtime: active=%#v selection=%#v", active, registry.Selection())
	}

	removed, found, promoted, promotedOK := registry.Remove("second", true)
	if !found || removed != second || !promotedOK || promoted != first {
		t.Fatalf("unexpected removal result: found=%t promoted=%t", found, promotedOK)
	}
	if active, ok = registry.Active(); !ok || active != first {
		t.Fatalf("expected first runtime to be promoted, got %#v", active)
	}

	cleared := registry.Clear("default")
	if len(cleared) != 1 || registry.IsUnlocked() {
		t.Fatalf("unexpected clear result: runtimes=%d unlocked=%t", len(cleared), registry.IsUnlocked())
	}
	if got := registry.Selection(); got.ID != "default" || got.Path != "/data/default.db" {
		t.Fatalf("unexpected reset selection: %#v", got)
	}
}

func TestRegistrySupportsConcurrentReadersDuringActivation(t *testing.T) {
	registry := NewRegistry("/data/default.db", "default", describeTestRuntime)
	var wait sync.WaitGroup
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				registry.IsUnlocked()
				registry.Selection()
				registry.Active()
				registry.Snapshot()
			}
		}()
	}
	for index := 0; index < 100; index++ {
		runtime := &testRuntime{id: "active", path: "/data/active.db"}
		registry.Activate(runtime)
		registry.Remove("active", false)
	}
	wait.Wait()
}
