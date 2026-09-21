package console

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParsePositiveInt(t *testing.T) {
	if got := parsePositiveInt("24", 80); got != 24 {
		t.Fatalf("expected parsed value, got %d", got)
	}
	if got := parsePositiveInt("0", 80); got != 80 {
		t.Fatalf("expected fallback for zero, got %d", got)
	}
	if got := parsePositiveInt("bad", 80); got != 80 {
		t.Fatalf("expected fallback for bad input, got %d", got)
	}
}

func TestConsoleIntervalLimiterWaitPreservesAcceptedInput(t *testing.T) {
	limiter := newConsoleIntervalLimiter(20 * time.Millisecond)
	if err := limiter.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := limiter.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 15*time.Millisecond {
		t.Fatalf("second accepted input was not delayed: %v", elapsed)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := limiter.wait(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait = %v", err)
	}
}

func TestConsoleIntervalLimiter(t *testing.T) {
	limiter := newConsoleIntervalLimiter(50 * time.Millisecond)
	if !limiter.allow() {
		t.Fatalf("first call should be allowed")
	}
	if limiter.allow() {
		t.Fatalf("second immediate call should be denied")
	}
	time.Sleep(60 * time.Millisecond)
	if !limiter.allow() {
		t.Fatalf("call after interval should be allowed")
	}
}
