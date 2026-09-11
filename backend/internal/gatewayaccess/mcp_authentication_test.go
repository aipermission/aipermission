package gatewayaccess

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type mcpTokenSourceFunc func(context.Context, string, time.Time) (tokens.Authentication, error)

func (fn mcpTokenSourceFunc) AuthenticateHash(ctx context.Context, hash string, now time.Time) (tokens.Authentication, error) {
	return fn(ctx, hash, now)
}

func TestMCPTokenParsingPrefersAPIKeyAndAcceptsBearer(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		apiKey        string
		authorization string
		want          string
	}{
		{name: "API key precedence", apiKey: " api-key ", authorization: "Bearer bearer-token", want: "api-key"},
		{name: "Bearer", authorization: "bEaReR   bearer-token ", want: "bearer-token"},
		{name: "unsupported authorization", authorization: "Basic value", want: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := newMCPAuthenticationRequest(t, testCase.apiKey)
			request.Header.Set("Authorization", testCase.authorization)
			if got := mcpTokenValue(request); got != testCase.want {
				t.Fatalf("token = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestMCPTokenRateLimitFingerprintIsStableAndNonReversible(t *testing.T) {
	const token = "aip_secret-token-value"
	first := mcpTokenRateLimitKey(token)
	second := mcpTokenRateLimitKey(token)
	if first != second || strings.Contains(first, token) || strings.Contains(first, "sha256:") {
		t.Fatalf("unsafe or unstable fingerprint: %q %q", first, second)
	}
	if len(strings.TrimPrefix(first, "mcp-token:")) != 24 {
		t.Fatalf("fingerprint = %q", first)
	}
}

func TestAuthenticateMCPOutcomesAndLimiterState(t *testing.T) {
	valid := mcpTokenSourceFunc(func(_ context.Context, hash string, _ time.Time) (tokens.Authentication, error) {
		if hash != tokens.HashToken("valid") {
			return tokens.Authentication{}, tokens.ErrNotFound
		}
		return tokens.Authentication{ID: 7, Name: "agent"}, nil
	})
	for _, testCase := range []struct {
		name       string
		token      string
		sources    []MCPTokenSource
		want       MCPAuthentication
		wantErr    error
		ipFailures int
		tokenFails int
	}{
		{name: "missing", wantErr: ErrMCPTokenMissing, ipFailures: 1},
		{name: "locked", token: "valid", wantErr: ErrMCPDatabaseLocked},
		{name: "invalid", token: "invalid", sources: []MCPTokenSource{valid}, wantErr: ErrMCPTokenInvalid, ipFailures: 1, tokenFails: 1},
		{name: "valid", token: "valid", sources: []MCPTokenSource{valid}, want: MCPAuthentication{TokenID: 7, Name: "agent", SourceIndex: 0}},
		{name: "duplicate", token: "valid", sources: []MCPTokenSource{valid, valid}, wantErr: ErrMCPTokenDuplicate},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			component := NewComponent("")
			request := newMCPAuthenticationRequest(t, testCase.token)
			got, err := component.AuthenticateMCP(request, func() []MCPTokenSource { return testCase.sources })
			if !errors.Is(err, testCase.wantErr) || got != testCase.want {
				t.Fatalf("authentication = %#v, %v", got, err)
			}
			if count := component.mcpIPLimiter.FailureCount("mcp:127.0.0.1"); count != testCase.ipFailures {
				t.Fatalf("IP failures = %d, want %d", count, testCase.ipFailures)
			}
			if testCase.token != "" {
				if count := component.mcpTokenLimiter.FailureCount(mcpTokenRateLimitKey(testCase.token)); count != testCase.tokenFails {
					t.Fatalf("token failures = %d, want %d", count, testCase.tokenFails)
				}
			}
		})
	}
}

func TestAuthenticateMCPSnapshotsOnceAfterBothWaits(t *testing.T) {
	component := NewComponent("")
	request := newMCPAuthenticationRequest(t, "valid")
	var snapshots atomic.Int32
	var observedHash string
	var observedNow time.Time
	result, err := component.AuthenticateMCP(request, func() []MCPTokenSource {
		snapshots.Add(1)
		return []MCPTokenSource{mcpTokenSourceFunc(func(_ context.Context, hash string, now time.Time) (tokens.Authentication, error) {
			observedHash, observedNow = hash, now
			return tokens.Authentication{ID: 1, Name: "one"}, nil
		})}
	})
	if err != nil || result.SourceIndex != 0 || snapshots.Load() != 1 {
		t.Fatalf("authentication = %#v, %v snapshots=%d", result, err, snapshots.Load())
	}
	if observedHash != tokens.HashToken("valid") || observedNow.Location() != time.UTC {
		t.Fatalf("hash/time = %q %v", observedHash, observedNow)
	}
}

func TestAuthenticateMCPStoreFailureDoesNotChangeLimiterState(t *testing.T) {
	component := NewComponent("")
	request := newMCPAuthenticationRequest(t, "valid")
	want := errors.New("storage unavailable")
	_, err := component.AuthenticateMCP(request, func() []MCPTokenSource {
		return []MCPTokenSource{mcpTokenSourceFunc(func(context.Context, string, time.Time) (tokens.Authentication, error) {
			return tokens.Authentication{}, want
		})}
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	if component.mcpIPLimiter.FailureCount("mcp:127.0.0.1") != 0 || component.mcpTokenLimiter.FailureCount(mcpTokenRateLimitKey("valid")) != 0 {
		t.Fatal("storage failure changed authentication limiter state")
	}
}

func TestAuthenticateMCPSuccessAndDuplicateResetOnlyTheirTokenBackoff(t *testing.T) {
	valid := mcpTokenSourceFunc(func(context.Context, string, time.Time) (tokens.Authentication, error) {
		return tokens.Authentication{ID: 1, Name: "agent"}, nil
	})
	for _, sources := range [][]MCPTokenSource{{valid}, {valid, valid}} {
		component := NewComponent("")
		key := mcpTokenRateLimitKey("valid")
		otherKey := mcpTokenRateLimitKey("other")
		component.mcpTokenLimiter.RecordFailure(key)
		component.mcpTokenLimiter.RecordFailure(otherKey)
		request := newMCPAuthenticationRequest(t, "valid")
		_, err := component.AuthenticateMCP(request, func() []MCPTokenSource { return sources })
		if err != nil && !errors.Is(err, ErrMCPTokenDuplicate) {
			t.Fatalf("authentication error = %v", err)
		}
		if component.mcpTokenLimiter.FailureCount(key) != 0 {
			t.Fatal("successful token backoff was retained")
		}
		if component.mcpTokenLimiter.FailureCount(otherKey) != 1 {
			t.Fatal("unrelated token backoff was changed")
		}
	}
}

func TestAuthenticateMCPCanceledWaitDoesNotSnapshot(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		lockoutKey func(*Component, *http.Request)
	}{
		{name: "IP", lockoutKey: func(component *Component, request *http.Request) {
			for range MCPGlobalLockoutFailures {
				component.mcpIPLimiter.RecordFailure("mcp:127.0.0.1")
			}
		}},
		{name: "token", lockoutKey: func(component *Component, request *http.Request) {
			for range AuthLockoutFailures {
				component.mcpTokenLimiter.RecordFailure(mcpTokenRateLimitKey(mcpTokenValue(request)))
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			component := NewComponent("")
			ctx, cancel := context.WithCancel(t.Context())
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
			request.RemoteAddr = "127.0.0.1:1234"
			request.Header.Set("X-API-Key", "valid")
			testCase.lockoutKey(component, request)
			cancel()
			called := false
			_, err := component.AuthenticateMCP(request, func() []MCPTokenSource { called = true; return nil })
			if !errors.Is(err, ErrMCPAuthenticationTimeout) || called {
				t.Fatalf("error=%v snapshot=%t", err, called)
			}
		})
	}
}

func TestAuthenticateMCPIsRaceSafe(t *testing.T) {
	component := NewComponent("")
	source := mcpTokenSourceFunc(func(context.Context, string, time.Time) (tokens.Authentication, error) {
		return tokens.Authentication{ID: 1, Name: "agent"}, nil
	})
	var workers sync.WaitGroup
	for range 50 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			request := newMCPAuthenticationRequest(t, "valid")
			if _, err := component.AuthenticateMCP(request, func() []MCPTokenSource { return []MCPTokenSource{source} }); err != nil {
				t.Errorf("authenticate: %v", err)
			}
		}()
	}
	workers.Wait()
}

func newMCPAuthenticationRequest(t testing.TB, token string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.RemoteAddr = "127.0.0.1:1234"
	if token != "" {
		request.Header.Set("X-API-Key", token)
	}
	return request
}
