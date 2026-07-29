package api

import (
	"testing"
	"time"
)

func TestWindowRateLimiterEnforcesIndependentKeys(t *testing.T) {
	limiter := newWindowRateLimiter(2, time.Minute)
	if !limiter.allow("workspace:token-1") || !limiter.allow("workspace:token-1") {
		t.Fatal("first two attempts should be allowed")
	}
	if limiter.allow("workspace:token-1") {
		t.Fatal("third attempt should be limited")
	}
	if !limiter.allow("workspace:token-2") {
		t.Fatal("a different token key should have an independent window")
	}
}
