package persistence

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBufferRetainsFailedPrefixAndConcurrentAppends(t *testing.T) {
	var buffer Buffer
	if err := buffer.Append(t.Context(), "first", nil); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("storage unavailable")
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- buffer.Drain(t.Context(), func(pending string) error {
			if pending != "first" {
				t.Errorf("pending = %q", pending)
			}
			close(entered)
			<-release
			return failure
		})
	}()
	<-entered
	if err := buffer.Append(t.Context(), "second", nil); err != nil {
		t.Fatal(err)
	}
	if buffer.Len() != len("firstsecond") {
		t.Fatal("in-flight bytes escaped capacity accounting")
	}
	close(release)
	if err := <-finished; !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var persisted string
	if err := buffer.Drain(t.Context(), func(pending string) error {
		persisted = pending
		return buffer.Append(t.Context(), "third", nil)
	}); err != nil {
		t.Fatal(err)
	}
	if persisted != "firstsecond" || buffer.Len() != len("third") {
		t.Fatalf("persisted=%q pending=%d", persisted, buffer.Len())
	}
	if err := buffer.Drain(t.Context(), func(pending string) error {
		if pending != "third" {
			t.Errorf("remaining prefix = %q", pending)
		}
		return nil
	}); err != nil || buffer.Len() != 0 {
		t.Fatalf("final drain: %v, pending=%d", err, buffer.Len())
	}
}

func TestBufferCapacityIncludesInflightAndCanceledProducer(t *testing.T) {
	var buffer Buffer
	accepted := strings.Repeat("a", MaxPendingBytes)
	if err := buffer.Append(t.Context(), accepted, nil); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- buffer.Drain(t.Context(), func(string) error {
			close(entered)
			<-release
			return errors.New("write failed")
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	requested := make(chan struct{}, 1)
	appended := make(chan error, 1)
	go func() {
		appended <- buffer.Append(ctx, "not accepted", func() { requested <- struct{}{} })
	}()
	<-requested
	cancel()
	if err := <-appended; !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked producer returned %v", err)
	}
	if buffer.Len() != MaxPendingBytes {
		t.Fatal("in-flight persistence allowed an unbounded append")
	}
	close(release)
	if err := <-finished; err == nil {
		t.Fatal("failed persistence returned success")
	}
	if err := buffer.Drain(t.Context(), func(pending string) error {
		if pending != accepted {
			t.Error("failed write changed accepted bytes")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBufferDrainAdmissionIsCancelable(t *testing.T) {
	var buffer Buffer
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- buffer.Drain(t.Context(), func(string) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := buffer.Drain(ctx, func(string) error {
		t.Error("canceled second writer entered persistence")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("second writer admission: %v", err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := buffer.Drain(ctx, func(string) error {
		t.Error("canceled idle writer entered persistence")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBufferBlockedProducerResumesAfterDurableWrite(t *testing.T) {
	var buffer Buffer
	if err := buffer.Append(t.Context(), strings.Repeat("a", MaxPendingBytes-1), nil); err != nil {
		t.Fatal(err)
	}
	requested := make(chan struct{}, 2)
	finished := make(chan error, 1)
	go func() {
		finished <- buffer.Append(t.Context(), "bc", func() { requested <- struct{}{} })
	}()
	<-requested
	if err := buffer.Drain(t.Context(), func(pending string) error {
		if len(pending) != MaxPendingBytes || !strings.HasSuffix(pending, "b") {
			t.Error("accepted partial prefix was not retained")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := buffer.Drain(t.Context(), func(pending string) error {
		if pending != "c" {
			t.Errorf("remaining producer output=%q", pending)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBufferSingleWorkerRetriesWithoutNewOutput(t *testing.T) {
	var buffer Buffer
	if err := buffer.Append(t.Context(), "once", nil); err != nil {
		t.Fatal(err)
	}
	if !buffer.StartWorker() || buffer.StartWorker() {
		t.Fatal("flush workers were not coalesced")
	}
	var attempts atomic.Int32
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		buffer.RunWorker(t.Context(), time.Millisecond, func() error {
			return buffer.Drain(t.Context(), func(pending string) error {
				if pending != "once" {
					t.Errorf("retry changed bytes=%q", pending)
				}
				if attempts.Add(1) == 1 {
					return errors.New("temporary failure")
				}
				return nil
			})
		})
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("worker did not retry without another append")
	}
	if attempts.Load() != 2 || buffer.Len() != 0 || !buffer.StartWorker() {
		t.Fatal("retry lost data or failed to release worker ownership")
	}
	buffer.FinishWorker(true)
}

func TestBufferWorkerStopsWithLifetimeAndDetachedWorkerIsSingleAttempt(t *testing.T) {
	for _, detached := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "detached"}[detached], func(t *testing.T) {
			var buffer Buffer
			if err := buffer.Append(t.Context(), "retained", nil); err != nil {
				t.Fatal(err)
			}
			buffer.StartWorker()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if detached {
				ctx = nil
			} else {
				cancel()
			}
			attempts := 0
			buffer.RunWorker(ctx, time.Millisecond, func() error {
				attempts++
				return errors.New("temporary failure")
			})
			want := 0
			if detached {
				want = 1
			}
			if attempts != want || buffer.Len() != len("retained") || !buffer.StartWorker() {
				t.Fatalf("attempts=%d pending=%d", attempts, buffer.Len())
			}
			buffer.FinishWorker(true)
		})
	}
}
