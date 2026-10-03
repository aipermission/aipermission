package persistence

import (
	"context"
	"errors"
	"testing"
)

func TestOutputGateCanceledWaitAndFinalTailAdmission(t *testing.T) {
	var gate OutputGate
	if err := gate.Lock(t.Context()); err != nil || gate.TryLock() {
		t.Fatalf("initial ownership: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := gate.Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait: %v", err)
	}
	gate.Unlock()
	// A detached final redactor tail may still enter an idle gate after the
	// transport lifetime ends; only a blocked wait requires a live context.
	if err := gate.Lock(ctx); err != nil {
		t.Fatalf("idle final-tail admission: %v", err)
	}
	finished := make(chan error, 1)
	go func() { finished <- gate.Lock(t.Context()) }()
	gate.Unlock()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	gate.Unlock()
}
