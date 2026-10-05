package console

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// Publish the terminal state exactly after waitReady observes "connecting".
// This reproduces shutdown winning the select without scheduler timing.
type readinessTransitionContext struct {
	context.Context
	once       sync.Once
	done       chan struct{}
	transition func()
}

func (ctx *readinessTransitionContext) Done() <-chan struct{} {
	ctx.once.Do(func() {
		ctx.transition()
		close(ctx.done)
	})
	return ctx.done
}

func TestConsoleReadinessPreservesFailureAtShutdown(t *testing.T) {
	for _, status := range []string{"error", "closed"} {
		t.Run(status, func(t *testing.T) {
			session := &managedConsoleSession{status: "connecting"}
			session.ctx = &readinessTransitionContext{
				Context: context.Background(), done: make(chan struct{}),
				transition: func() {
					session.mu.Lock()
					defer session.mu.Unlock()
					session.status = status
					session.errText = "transport dial: connection refused [REDACTED]"
				},
			}
			if err := session.waitReady(t.Context()); err == nil || err.Error() != "transport dial: connection refused [REDACTED]" {
				t.Fatalf("shutdown lost the recorded failure: %v", err)
			}
		})
	}
}

func TestConsoleReadinessShutdownWithoutFailure(t *testing.T) {
	session := &managedConsoleSession{status: "connecting"}
	session.ctx = &readinessTransitionContext{
		Context: context.Background(), done: make(chan struct{}), transition: func() {},
	}
	if err := session.waitReady(t.Context()); err == nil || !strings.Contains(err.Error(), "console session closed") {
		t.Fatalf("shutdown without recorded failure = %v", err)
	}
}

func TestConsoleReadinessHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	session := &managedConsoleSession{
		ctx: context.Background(), status: "error", errText: "connection refused",
	}
	if err := session.waitReady(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation = %v", err)
	}
}
