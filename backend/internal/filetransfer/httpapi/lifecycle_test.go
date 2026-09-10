package filetransferhttp

import (
	"context"
	"testing"
)

func TestLifecycleOwnsRegistryAndFinalizationLifetime(t *testing.T) {
	lifecycle := NewLifecycle()
	if lifecycle.Registry() == nil || !lifecycle.finalization.Valid() {
		t.Fatal("new lifecycle is incomplete")
	}
	if !lifecycle.Wait(t.Context()) {
		t.Fatal("empty lifecycle did not drain")
	}
	lifecycle.Stop()
	select {
	case <-lifecycle.finalization.Context().Done():
	default:
		t.Fatal("stopped lifecycle retained its finalization context")
	}
}

func TestNilLifecycleFailsClosed(t *testing.T) {
	var lifecycle *Lifecycle
	if lifecycle.Registry() != nil || !lifecycle.Wait(context.Background()) {
		t.Fatal("nil lifecycle did not remain inert")
	}
	lifecycle.Stop()
}
