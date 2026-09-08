package transferjobs

import "testing"

func TestFinalizationLifetimeEndsWhenStopped(t *testing.T) {
	lifetime := NewFinalizationLifetime()
	ctx := lifetime.Context()
	select {
	case <-ctx.Done():
		t.Fatal("new finalization lifetime is canceled")
	default:
	}
	lifetime.Stop()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("finalization lifetime remained active after stop")
	}
}
