package gatewayconnectorapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/gorilla/websocket"
)

func TestEnvironmentErrorsUseConnectorNeutralPresentation(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		present func(error) (int, string, bool)
		status  int
		body    string
	}{
		{name: "unsupported", err: connectors.ErrSessionEnvironmentUnsupported, status: http.StatusConflict, body: "does not support"},
		{name: "validation", err: errors.New("invalid selection"), present: func(error) (int, string, bool) {
			return http.StatusBadRequest, "invalid selection", true
		}, status: http.StatusBadRequest, body: "invalid selection"},
		{name: "not found", err: errors.New("missing"), present: func(error) (int, string, bool) {
			return http.StatusNotFound, "vault item not found", true
		}, status: http.StatusNotFound, body: "vault item not found"},
		{name: "stale", err: errors.New("stale"), present: func(error) (int, string, bool) {
			return http.StatusConflict, "refresh and try again", true
		}, status: http.StatusConflict, body: "refresh and try again"},
		{name: "unknown", err: errors.New("unknown"), present: func(error) (int, string, bool) {
			return 0, "", false
		}, status: http.StatusInternalServerError, body: "internal server error"},
		{name: "invalid presentation", err: errors.New("unknown"), present: func(error) (int, string, bool) {
			return http.StatusOK, "misclassified", true
		}, status: http.StatusInternalServerError, body: "internal server error"},
		{name: "missing presenter", err: errors.New("unknown"), status: http.StatusInternalServerError, body: "internal server error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeEnvironmentError(response, test.present, test.err)
			if response.Code != test.status || !strings.Contains(strings.ToLower(response.Body.String()), test.body) {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}

type fakeSessions struct {
	items         []console.Record
	created       console.Record
	createRequest console.CreateRequest
	createErr     error
	inputErr      error
	closeErr      error
	attachErr     error
	runtimeID     int64
}

type liveConsoleTestErrorPresenter struct{}

func (liveConsoleTestErrorPresenter) WriteConnectorError(w http.ResponseWriter, _ error) bool {
	http.Error(w, "approve host key", http.StatusConflict)
	return true
}

func (liveConsoleTestErrorPresenter) ConnectorErrorMessage(prefix string, err error) string {
	return prefix + ": " + err.Error()
}

func (sessions *fakeSessions) List(context.Context, int64) ([]console.Record, error) {
	return sessions.items, nil
}

func (sessions *fakeSessions) Create(_ context.Context, request console.CreateRequest) (console.Record, error) {
	sessions.createRequest = request
	return sessions.created, sessions.createErr
}

func (sessions *fakeSessions) Get(context.Context, int64) (console.Record, error) {
	if len(sessions.items) == 0 {
		return console.Record{}, console.ErrNotFound
	}
	return sessions.items[0], nil
}

func (sessions *fakeSessions) Input(context.Context, executionprincipal.Principal, int64, string) error {
	return sessions.inputErr
}

func (sessions *fakeSessions) Close(context.Context, executionprincipal.Principal, int64) error {
	return sessions.closeErr
}

func (sessions *fakeSessions) RuntimeID(context.Context, int64) (int64, error) {
	return sessions.runtimeID, nil
}

func (sessions *fakeSessions) Attach(http.ResponseWriter, *http.Request, executionprincipal.Principal, int64, func(http.ResponseWriter, *http.Request) (*websocket.Conn, error)) error {
	return sessions.attachErr
}

func testRuntime(t *testing.T, sessions *fakeSessions) *LiveConsoleHTTPRuntime {
	t.Helper()
	principal, err := executionprincipal.LocalOperator("workspace", "runtime")
	if err != nil {
		t.Fatal(err)
	}
	return &LiveConsoleHTTPRuntime{Sessions: sessions, Principal: func() (executionprincipal.Principal, error) { return principal, nil }}
}

func request(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func TestCreateBuildsEnvironmentAndObservesSession(t *testing.T) {
	sessions := &fakeSessions{created: console.Record{ID: 7, RuntimeID: 9, Name: "shell"}}
	runtime := testRuntime(t, sessions)
	runtime.PlanEnvironment = func(_ context.Context, runtimeID int64, selections []LiveConsoleVaultSelection) (LiveConsoleEnvironmentPlan, error) {
		if runtimeID != 9 || len(selections) != 1 || selections[0] != (LiveConsoleVaultSelection{
			ItemID: 12, SourceProjectID: 3, ReplaceExisting: true, BindingID: 4, BindingRevision: 5,
		}) {
			t.Fatalf("environment request = runtime %d selections %#v", runtimeID, selections)
		}
		return LiveConsoleEnvironmentPlan{ItemIDs: []int64{12}, ContentHash: "content", Prepare: func(context.Context, string) (console.EnvironmentPreparation, error) {
			return console.EnvironmentPreparation{}, nil
		}}, nil
	}
	var observedAction string
	runtime.Observe = func(_ context.Context, runtimeID int64, action string, payload map[string]any) {
		if runtimeID != 9 || payload["environment_content_hash"] != "content" {
			t.Fatalf("unexpected observation: runtime=%d payload=%#v", runtimeID, payload)
		}
		observedAction = action
	}
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	handlers.Create(response, request(http.MethodPost, "/api/console/sessions", `{"runtime_id":9,"name":"shell","vault_items":[{"item_id":12,"source_project_id":3,"replace_existing":true,"binding_id":4,"binding_revision":5}]}`))
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !sessions.createRequest.WaitForStart || sessions.createRequest.Principal.Validate() != nil || sessions.createRequest.PrepareEnvironment == nil {
		t.Fatalf("create request did not preserve session invariants: %#v", sessions.createRequest)
	}
	if observedAction != "console.session.created_observed" {
		t.Fatalf("observation action = %q", observedAction)
	}
}

func TestCreatePreservesConnectorErrorResponse(t *testing.T) {
	sessions := &fakeSessions{createErr: errors.New("host key changed")}
	runtime := testRuntime(t, sessions)
	runtime.ErrorAdapter = func(_ context.Context, runtimeID int64) ErrorPresenter {
		if runtimeID != 9 {
			t.Fatalf("unexpected error presentation input")
		}
		return liveConsoleTestErrorPresenter{}
	}
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	handlers.Create(response, request(http.MethodPost, "/api/console/sessions", `{"runtime_id":9}`))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "approve host key") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateRejectsMalformedJSONBeforeResolvingPrincipal(t *testing.T) {
	sessions := &fakeSessions{}
	runtime := testRuntime(t, sessions)
	principalCalls := 0
	runtime.Principal = func() (executionprincipal.Principal, error) {
		principalCalls++
		return executionprincipal.Principal{}, errors.New("unavailable")
	}
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	handlers.Create(response, request(http.MethodPost, "/api/console/sessions", `{"runtime_id":`))
	if response.Code != http.StatusBadRequest || principalCalls != 0 {
		t.Fatalf("status=%d principal calls=%d body=%s", response.Code, principalCalls, response.Body.String())
	}
}

func TestCloseCancelsRunningRequestBeforeSuccess(t *testing.T) {
	sessions := &fakeSessions{runtimeID: 17}
	runtime := testRuntime(t, sessions)
	canceled := false
	runtime.CancelForSession = func(_ context.Context, sessionID int64, reason string) error {
		canceled = sessionID == 5 && strings.Contains(reason, "closed")
		return nil
	}
	runtime.Observe = func(_ context.Context, runtimeID int64, action string, _ map[string]any) {
		if runtimeID != 17 || action != "console.session.closed_observed" {
			t.Fatalf("unexpected observation runtime=%d action=%s", runtimeID, action)
		}
	}
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	req := request(http.MethodPost, "/api/console/sessions/5/close", "")
	req.SetPathValue("id", "5")
	handlers.Close(response, req)
	if response.Code != http.StatusOK || !canceled {
		t.Fatalf("status=%d canceled=%t body=%s", response.Code, canceled, response.Body.String())
	}
}

func TestPathValidationFollowsWorkspaceScope(t *testing.T) {
	scopeCalls := 0
	handlers := NewLiveConsoleHTTPHandlers(func(w http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) {
		scopeCalls++
		http.Error(w, "locked", http.StatusLocked)
		return nil, false
	})
	response := httptest.NewRecorder()
	req := request(http.MethodGet, "/api/console/sessions/nope", "")
	req.SetPathValue("id", "nope")
	handlers.Get(response, req)
	if response.Code != http.StatusLocked || scopeCalls != 1 {
		t.Fatalf("status=%d scope calls=%d", response.Code, scopeCalls)
	}
}

func TestAttachMapsInactiveSessionToConflict(t *testing.T) {
	sessions := &fakeSessions{attachErr: console.InactiveError{Status: "closed"}}
	runtime := testRuntime(t, sessions)
	runtime.UpgradeWebSocket = func(http.ResponseWriter, *http.Request) (*websocket.Conn, error) { return nil, nil }
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	req := request(http.MethodGet, "/api/console/sessions/4/attach", "")
	req.SetPathValue("id", "4")
	handlers.Attach(response, req)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "closed") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
