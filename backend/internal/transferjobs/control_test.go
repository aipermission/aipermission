package transferjobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestControlTransitionsAndCanceledWaits(t *testing.T) {
	var control Control
	if err := control.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if control.Resume() || !control.Pause() || control.Pause() {
		t.Fatal("invalid pause transition")
	}
	paused := control.resume
	for i := 0; i < 1000; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := control.Wait(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled wait = %v", err)
		}
	}
	if control.resume != paused {
		t.Fatal("canceled wait changed the shared pause gate")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := control.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("paused wait = %v", err)
	}
	if !control.Resume() || control.Resume() {
		t.Fatal("invalid resume transition")
	}
	if err := control.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("resuming erased cancellation")
	}
	if err := control.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestResumeReleasesAllWaitersAcrossCycles(t *testing.T) {
	var control Control
	for cycle := 0; cycle < 10; cycle++ {
		control.Pause()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		results := make(chan error, 30)
		ready := make(chan struct{}, 30)
		for range 30 {
			go func() { ready <- struct{}{}; results <- control.Wait(ctx) }()
		}
		for range 30 {
			<-ready
		}
		select {
		case err := <-results:
			t.Fatalf("paused gate released waiter: %v", err)
		default:
		}
		control.Resume()
		for range 30 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		cancel()
	}
}

func TestConcurrentControlTransitions(t *testing.T) {
	var control Control
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			for range 20 {
				control.Pause()
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_ = control.Wait(ctx)
				control.Resume()
			}
		})
	}
	workers.Wait()
	control.Resume()
	if err := control.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}
