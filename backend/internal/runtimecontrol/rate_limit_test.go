package runtimecontrol

import (
	"context"
	"testing"
	"time"
)

func TestWindowEnforcesIndependentKeys(t *testing.T) {
	limiter := NewWindow(2, time.Minute)
	if !limiter.Allow("workspace:token-1") || !limiter.Allow("workspace:token-1") {
		t.Fatal("first two attempts should be allowed")
	}
	if limiter.Allow("workspace:token-1") {
		t.Fatal("third attempt should be limited")
	}
	if !limiter.Allow("workspace:token-2") {
		t.Fatal("a different token key should have an independent window")
	}
}

func TestAuthenticationBackoffIsIndependent(t *testing.T) {
	limiter := NewAuth(1, 8)
	for range 8 {
		limiter.RecordFailure("broken-client")
	}
	if limiter.Delay("broken-client") < 50*time.Second {
		t.Fatalf("broken client delay = %s, want lockout", limiter.Delay("broken-client"))
	}
	if delay := limiter.Delay("valid-client"); delay != 0 {
		t.Fatalf("valid client inherited another token's delay: %s", delay)
	}
}

func TestAuthenticationLimiterUsesConfiguredThreshold(t *testing.T) {
	limiter := NewAuth(32, 64)
	for range 31 {
		limiter.RecordFailure("mcp:127.0.0.1")
	}
	if delay := limiter.Delay("mcp:127.0.0.1"); delay != 0 {
		t.Fatalf("limiter delayed normal clients too early: %s", delay)
	}
	limiter.RecordFailure("mcp:127.0.0.1")
	if delay := limiter.Delay("mcp:127.0.0.1"); delay <= 0 {
		t.Fatal("limiter did not activate at its configured threshold")
	}
}

func TestAuthenticationLimiterEvictsOldestEntriesAtBound(t *testing.T) {
	limiter := NewAuth(1, 8)
	for index := 0; index < maxAuthEntries+20; index++ {
		limiter.RecordFailure(string(rune(index + 1)))
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if len(limiter.entries) > maxAuthEntries {
		t.Fatalf("limiter retained %d entries, want at most %d", len(limiter.entries), maxAuthEntries)
	}
}

func TestWaitHonorsCancellation(t *testing.T) {
	limiter := NewAuth(1, 8)
	limiter.RecordFailure("client")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.Wait(ctx, "client"); err == nil {
		t.Fatal("canceled wait should fail")
	}
}
