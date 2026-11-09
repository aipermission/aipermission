package idlecleanup

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestHourlyWorkerRunsAtBoundaryWithoutPollingAndCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var count atomic.Int64
		done := make(chan struct{})
		go func() { defer close(done); runHourly(ctx, func() { count.Add(1) }) }()
		synctest.Wait()
		delay := time.Until(nextHour(time.Now()))
		time.Sleep(delay - time.Nanosecond)
		synctest.Wait()
		if count.Load() != 0 {
			t.Fatal("cleanup before first hour boundary")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if count.Load() != 1 {
			t.Fatal("missed hour boundary")
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if count.Load() != 2 {
			t.Fatal("duplicate or missing hourly run")
		}
		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("worker did not stop")
		}
		time.Sleep(3 * time.Hour)
		synctest.Wait()
		if count.Load() != 2 {
			t.Fatal("cleanup continued after workspace cancellation")
		}
	})
}
