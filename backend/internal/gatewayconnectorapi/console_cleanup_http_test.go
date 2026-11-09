package gatewayconnectorapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/console/idlecleanup"
)

type cleanupTestSessions struct {
	fakeSessions
	policy  idlecleanup.Policy
	updates int
	fail    bool
}

func (s *cleanupTestSessions) CleanupPolicy(context.Context) (idlecleanup.Policy, error) {
	if s.fail {
		return idlecleanup.Policy{}, errors.New("failed")
	}
	return s.policy, nil
}
func (s *cleanupTestSessions) UpdateCleanupPolicy(ctx context.Context, p idlecleanup.Policy, mutate auditedmutation.Runner) error {
	if s.fail {
		return errors.New("failed")
	}
	if err := mutate(ctx, "console.cleanup.policy_updated", func() any { return p }, func(*sql.Tx) error { return nil }); err != nil {
		return err
	}
	s.updates++
	s.policy = p
	return nil
}

func TestCleanupSettingsHTTPValidationAndPrivilege(t *testing.T) {
	s := &cleanupTestSessions{policy: idlecleanup.Policy{IdleMinutes: 60}}
	runtime := testRuntime(t, &s.fakeSessions)
	runtime.Sessions = s
	audited := 0
	runtime.Mutate = func(_ context.Context, action string, payload func() any, _ func(*sql.Tx) error) error {
		if action != "console.cleanup.policy_updated" || payload() == nil {
			t.Fatal("missing audit")
		}
		audited++
		return nil
	}
	h := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	h.CleanupSettings(response, request("GET", "/api/settings/console-cleanup", ""))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	for _, body := range []string{`{}`, `{"enabled":null,"idle_minutes":60}`, `{"enabled":true}`, `{"enabled":true,"idle_minutes":null}`, `{"enabled":true,"idle_minutes":0}`, `{"enabled":true,"idle_minutes":11}`, `{"enabled":"true","idle_minutes":30}`} {
		response = httptest.NewRecorder()
		h.UpdateCleanupSettings(response, request("PUT", "/api/settings/console-cleanup", body))
		if response.Code != 400 || s.updates != 0 {
			t.Fatalf("%s %d", body, response.Code)
		}
	}
	for _, body := range []string{`{"enabled":true,"idle_minutes":30}`, `{"enabled":false,"idle_minutes":120}`} {
		response = httptest.NewRecorder()
		h.UpdateCleanupSettings(response, request("PUT", "/api/settings/console-cleanup", body))
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
	}
	if audited != 2 || s.updates != 2 || s.policy.Enabled || s.policy.IdleMinutes != 120 {
		t.Fatalf("%d %+v", audited, s.policy)
	}
	runtime.Principal = func() (Principal, error) {
		return Principal{Kind: PrincipalMCPToken, TokenID: 1, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}, nil
	}
	response = httptest.NewRecorder()
	h.UpdateCleanupSettings(response, request("PUT", "/api/settings/console-cleanup", `{"enabled":true,"idle_minutes":10}`))
	if response.Code != 403 || audited != 2 {
		t.Fatal("token changed operator policy")
	}
}

func TestCleanupSettingsHTTPFailsClosed(t *testing.T) {
	s := &cleanupTestSessions{fail: true}
	runtime := testRuntime(t, &s.fakeSessions)
	runtime.Sessions = s
	h := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	h.CleanupSettings(response, request("GET", "/api/settings/console-cleanup", ""))
	if response.Code != 500 {
		t.Fatal(response.Code)
	}
	response = httptest.NewRecorder()
	h.UpdateCleanupSettings(response, request("PUT", "/api/settings/console-cleanup", `{"enabled":true,"idle_minutes":60}`))
	if response.Code != 500 {
		t.Fatal(response.Code)
	}
}
