package providerlock

import (
	"context"
	"errors"
	"testing"
)

func TestManagerSerializesOneProviderOnly(t *testing.T) {
	var manager Manager
	release, err := manager.Acquire(t.Context(), nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	other, err := manager.Acquire(t.Context(), nil, 2)
	if err != nil {
		t.Fatalf("unrelated provider was blocked: %v", err)
	}
	other()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if blocked, err := manager.Acquire(canceled, nil, 1); blocked != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked acquire = release:%v err:%v", blocked != nil, err)
	}
	release()
}
