package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

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

func TestMCPAuthenticationAcceptsBearerToken(t *testing.T) {
	fixture := newAPITestFixture(t)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "bearer-client"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/mcp/connector-targets", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Host = "localhost"
	request.Header.Set("Authorization", "bEaReR "+token.TokenValue)
	response := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Bearer authentication = %d %s", response.Code, response.Body.String())
	}
}

func TestMCPAuthenticationErrorHTTPMapping(t *testing.T) {
	for _, testCase := range []struct {
		err     error
		status  int
		message string
	}{
		{err: gatewayaccess.ErrMCPAuthenticationTimeout, status: http.StatusRequestTimeout, message: "authentication request timed out"},
		{err: gatewayaccess.ErrMCPTokenMissing, status: http.StatusUnauthorized, message: "missing API token"},
		{err: gatewayaccess.ErrMCPTokenInvalid, status: http.StatusUnauthorized, message: "invalid, revoked, or expired API token"},
		{err: gatewayaccess.ErrMCPTokenDuplicate, status: http.StatusConflict, message: "matches multiple unlocked databases"},
		{err: gatewayaccess.ErrMCPDatabaseLocked, status: http.StatusLocked, message: "database is locked"},
		{err: errors.New("storage failed"), status: http.StatusInternalServerError, message: "internal server error"},
	} {
		response := httptest.NewRecorder()
		writeMCPAuthenticationError(response, testCase.err)
		if response.Code != testCase.status || !strings.Contains(response.Body.String(), testCase.message) {
			t.Errorf("error %v = %d %s", testCase.err, response.Code, response.Body.String())
		}
	}
}
