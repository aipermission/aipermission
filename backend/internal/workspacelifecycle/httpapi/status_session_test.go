package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

func TestStatusSessionFlagMatchesRequestAuthentication(t *testing.T) {
	cases := []struct {
		name, state, wantState string
		hasSession, configured bool
		wantAuthenticated      bool
		wantChecks             int
	}{
		{name: "no_session_policy", state: "unlocked", wantState: "session_required"},
		{name: "missing_session", state: "unlocked", configured: true, wantState: "session_required", wantChecks: 1},
		{name: "authenticated", state: "unlocked", configured: true, hasSession: true, wantState: "unlocked", wantAuthenticated: true, wantChecks: 1},
		{name: "locked", state: "locked", configured: true, hasSession: true, wantState: "locked"},
		{name: "setup_required", state: "setup_required", configured: true, hasSession: true, wantState: "setup_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checks := 0
			dependencies := Dependencies{Lifecycle: &fakeLifecycle{status: workspacelifecycle.Status{
				State: tc.state, Identity: workspacelifecycle.Identity{ID: "selected"}, DatabaseName: "Selected",
			}}}
			if tc.configured {
				dependencies.HasSession = func(*http.Request) bool { checks++; return tc.hasSession }
			}
			response := httptest.NewRecorder()
			New(dependencies).Status(response, httptest.NewRequest(http.MethodGet, "/api/unlock/status", nil))
			var status StatusResponse
			if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || status.State != tc.wantState || status.UISessionAuthenticated != tc.wantAuthenticated || checks != tc.wantChecks {
				t.Fatalf("status=%d/%s authenticated=%t checks=%d", response.Code, status.State, status.UISessionAuthenticated, checks)
			}
			if status.DatabaseID != "selected" || status.DatabaseName != "Selected" {
				t.Fatal("session projection changed workspace identity")
			}
		})
	}
}
