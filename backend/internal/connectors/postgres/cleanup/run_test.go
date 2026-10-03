package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunDetachesCancellationPreservesValuesAndBoundsCleanup(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), key{}, "fixture-value"))
	cancel()
	failure := errors.New("fixture cleanup error")
	var cleanup context.Context
	started := time.Now()
	err := Run(ctx, func(ctx context.Context) error {
		cleanup = ctx
		deadline, bounded := ctx.Deadline()
		if ctx.Err() != nil || ctx.Value(key{}) != "fixture-value" || !bounded || deadline.Before(started) || deadline.After(time.Now().Add(Timeout)) {
			t.Fatalf("invalid cleanup context: err=%v deadline=%v bounded=%v", ctx.Err(), deadline, bounded)
		}
		return failure
	})
	if !errors.Is(err, failure) || cleanup.Err() != context.Canceled {
		t.Fatalf("cleanup result/cancellation lost: err=%v context=%v", err, cleanup.Err())
	}
}
