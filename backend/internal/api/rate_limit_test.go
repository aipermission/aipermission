package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/tokens"
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

func TestMCPTokenRateLimitFingerprintIsNonReversibleAndStable(t *testing.T) {
	const token = "aip_secret-token-value"
	first := mcpTokenRateLimitKey(token)
	second := mcpTokenRateLimitKey(token)
	if first != second {
		t.Fatalf("fingerprint is not stable: %q != %q", first, second)
	}
	if strings.Contains(first, token) || len(first) >= len("mcp-token:")+64 {
		t.Fatalf("fingerprint exposes raw or full token hash: %q", first)
	}
}

func TestMCPTokenAuthenticationBackoffIsIndependent(t *testing.T) {
	limiter := newAuthRateLimiter()
	brokenClient := mcpTokenRateLimitKey("broken-client-token")
	validClient := mcpTokenRateLimitKey("valid-client-token")
	for range authRateLimitLockoutFailures {
		limiter.recordFailure(brokenClient)
	}
	if limiter.delay(brokenClient) < 50*time.Second {
		t.Fatalf("broken client delay = %s, want lockout", limiter.delay(brokenClient))
	}
	if delay := limiter.delay(validClient); delay != 0 {
		t.Fatalf("valid client inherited another token's delay: %s", delay)
	}
}

func TestMCPGlobalAuthenticationLimiterUsesCoarseThreshold(t *testing.T) {
	limiter := newMCPGlobalAuthRateLimiter()
	for range mcpGlobalDelayFailures - 1 {
		limiter.recordFailure("mcp:127.0.0.1")
	}
	if delay := limiter.delay("mcp:127.0.0.1"); delay != 0 {
		t.Fatalf("coarse limiter delayed normal clients too early: %s", delay)
	}
	limiter.recordFailure("mcp:127.0.0.1")
	if delay := limiter.delay("mcp:127.0.0.1"); delay <= 0 {
		t.Fatal("coarse limiter did not activate at its aggregate threshold")
	}
}

func TestAuthRateLimiterEvictsOldestEntriesAtBound(t *testing.T) {
	limiter := newAuthRateLimiter()
	for index := 0; index < maxAuthRateLimitEntries+20; index++ {
		limiter.recordFailure(mcpTokenRateLimitKey(string(rune(index + 1))))
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if len(limiter.entries) > maxAuthRateLimitEntries {
		t.Fatalf("limiter retained %d entries, want at most %d", len(limiter.entries), maxAuthRateLimitEntries)
	}
}

func TestMCPAuthenticationDoesNotShareTokenBackoff(t *testing.T) {
	fixture := newAPITestFixture(t)
	validToken, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "valid-client"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	brokenKey := mcpTokenRateLimitKey("broken-client-token")
	for range authRateLimitLockoutFailures {
		fixture.server.mcpTokenAuthLimiter.recordFailure(brokenKey)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", validToken.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("valid client inherited broken token backoff: %d %s", response.Code, response.Body.String())
	}
}

func TestMCPMissingTokenDoesNotCreateFingerprintEntry(t *testing.T) {
	fixture := newAPITestFixture(t)
	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", "", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token response = %d, want 401", response.Code)
	}
	fixture.server.mcpTokenAuthLimiter.mu.Lock()
	defer fixture.server.mcpTokenAuthLimiter.mu.Unlock()
	if len(fixture.server.mcpTokenAuthLimiter.entries) != 0 {
		t.Fatalf("missing token created %d fingerprint entries", len(fixture.server.mcpTokenAuthLimiter.entries))
	}
}
