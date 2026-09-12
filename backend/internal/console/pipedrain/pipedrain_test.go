package pipedrain

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitIsBounded(t *testing.T) {
	group := New(false)
	if group.Wait(time.Millisecond) {
		t.Fatal("blocked pipe was reported as drained")
	}
	group.Done()
	if !group.Wait(time.Second) {
		t.Fatal("completed pipe was not reported as drained")
	}
}

func TestNewTracksBothOutputPipes(t *testing.T) {
	group := New(true)
	group.Done()
	if group.Wait(time.Millisecond) {
		t.Fatal("stdout completion drained a group that still owns stderr")
	}
	group.Done()
	if !group.Wait(time.Second) {
		t.Fatal("stdout and stderr completion did not drain the group")
	}
}

func TestFinishWipesAfterBoundedWaitWithoutWaitingForBlockedReader(t *testing.T) {
	group := New(false)
	var finishes atomic.Int32
	started := time.Now()
	group.Finish(time.Millisecond, func() { finishes.Add(1) })
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Finish retained the caller for %v", elapsed)
	}
	if finishes.Load() != 1 {
		t.Fatalf("finish callback count = %d", finishes.Load())
	}
	group.Done()
	time.Sleep(time.Millisecond)
	if finishes.Load() != 1 {
		t.Fatalf("late drain repeated finish callback: %d", finishes.Load())
	}
}
