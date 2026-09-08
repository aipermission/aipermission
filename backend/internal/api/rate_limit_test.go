package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

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
	if strings.Contains(first, "sha256:") || len(strings.TrimPrefix(first, "mcp-token:")) != 24 {
		t.Fatalf("fingerprint should contain exactly 24 digest characters: %q", first)
	}
}

func TestDatabasePasswordRateLimitKeyIsSharedAcrossRoutesAndOmitsRequestMetadata(t *testing.T) {
	first := httptest.NewRequest(http.MethodPost, "/api/unlock", strings.NewReader(`{"password":"first-secret"}`))
	first.RemoteAddr = "127.0.0.1:12345"
	second := httptest.NewRequest(http.MethodPost, "/api/databases/delete-locked?database=customer-name", strings.NewReader(`{"current_password":"second-secret"}`))
	second.RemoteAddr = "127.0.0.1:54321"

	firstKey := runtimecontrol.Key(first, databasePasswordRateLimitScope)
	secondKey := runtimecontrol.Key(second, databasePasswordRateLimitScope)
	if firstKey != secondKey {
		t.Fatalf("database password routes use different keys: %q != %q", firstKey, secondKey)
	}
	for _, sensitive := range []string{"unlock", "delete-locked", "customer-name", "first-secret", "second-secret"} {
		if strings.Contains(firstKey, sensitive) {
			t.Fatalf("rate-limit key exposes request metadata %q: %q", sensitive, firstKey)
		}
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
		fixture.server.mcpTokenAuthLimiter.RecordFailure(brokenKey)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", validToken.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("valid client inherited broken token backoff: %d %s", response.Code, response.Body.String())
	}
}

func TestMCPMissingTokenDoesNotDelayAValidToken(t *testing.T) {
	fixture := newAPITestFixture(t)
	for range authRateLimitLockoutFailures {
		response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", "", nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("missing token response = %d, want 401", response.Code)
		}
	}
	validToken, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "valid-after-missing"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", validToken.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("missing-token attempts affected valid token limiter: %d %s", response.Code, response.Body.String())
	}
}
