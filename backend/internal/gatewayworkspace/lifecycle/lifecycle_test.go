package lifecycle

import (
	"context"
	"errors"
	"testing"
)

func TestUnconfiguredLifecycleFailsClosed(t *testing.T) {
	components := []*Component{nil, NewComponent(t.TempDir(), "default", nil)}
	for _, component := range components {
		release, err := component.AcquireReadContext(context.Background())
		if release != nil || !errors.Is(err, InitializationError()) {
			t.Fatalf("AcquireReadContext() = (release present: %t, %v), want nil initialization error", release != nil, err)
		}
		release, err = component.AcquireMutationContext(context.Background())
		if release != nil || !errors.Is(err, InitializationError()) {
			t.Fatalf("AcquireMutationContext() = (release present: %t, %v), want nil initialization error", release != nil, err)
		}
		if err := component.CloseAll(context.Background()); !errors.Is(err, InitializationError()) {
			t.Fatalf("CloseAll() error = %v, want initialization error", err)
		}
	}
}
