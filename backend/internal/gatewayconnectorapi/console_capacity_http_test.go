package gatewayconnectorapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
)

type capacityTestSessions struct {
	fakeSessions
	after   uint64
	closed  []int64
	onClose func()
}

func (sessions *capacityTestSessions) Capacity(_ context.Context) (capacity.Report, error) {
	return capacity.Report{Limit: 32, Available: 32, Revision: 1, Items: []capacity.Session{}}, nil
}

func (sessions *capacityTestSessions) WaitCapacity(ctx context.Context, after uint64) (uint64, error) {
	if after == 0 {
		return 1, nil
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

func (sessions *capacityTestSessions) CloseCapacitySession(_ context.Context, _ Principal, item capacity.Selection, _ int) error {
	sessions.closed = append(sessions.closed, item.ID)
	if sessions.onClose != nil {
		sessions.onClose()
	}
	if item.ID == 2 {
		return ErrLiveConsoleChanged
	}
	if item.ID == 3 {
		return context.DeadlineExceeded
	}
	return nil
}

func TestCapacityCleanupAuditSurvivesCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sessions := &capacityTestSessions{onClose: cancel}
	runtime := testRuntime(t, &sessions.fakeSessions)
	runtime.Sessions = sessions
	observed := false
	runtime.Observe = func(audit context.Context, _ int64, _ string, _ map[string]any) {
		observed = true
		if audit.Err() != nil {
			t.Fatal("audit inherited caller cancellation")
		}
	}
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	r := request("POST", "/api/console/capacity/close", `{"idle_minutes":0,"sessions":[{"id":1,"generation":1,"last_activity":"2026-11-08T11:00:00Z"}]}`).WithContext(ctx)
	handlers.CloseCapacity(httptest.NewRecorder(), r)
	if !observed {
		t.Fatal("missing cleanup audit")
	}
}

func TestConsoleCapacityHTTP(t *testing.T) {
	sessions := &capacityTestSessions{}
	runtime := testRuntime(t, &sessions.fakeSessions)
	runtime.Sessions = sessions
	observed := false
	runtime.Observe = func(_ context.Context, _ int64, action string, _ map[string]any) {
		observed = action == "console.capacity.cleanup"
	}
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	handlers.Capacity(response, httptest.NewRequest("GET", "/api/console/capacity", nil))
	if response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	body := `{"idle_minutes":30,"sessions":[{"id":1,"generation":1,"last_activity":"2026-11-08T11:00:00Z"},{"id":2,"generation":1,"last_activity":"2026-11-08T11:00:00Z"},{"id":3,"generation":1,"last_activity":"2026-11-08T11:00:00Z"}]}`
	handlers.CloseCapacity(response, request("POST", "/api/console/capacity/close", body))
	var result capacity.CloseResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(result.Closed) != 1 || len(result.Skipped) != 1 || len(result.Uncertain) != 1 || !observed {
		t.Fatalf("%d %+v audit=%v body=%s", response.Code, result, observed, response.Body.String())
	}
}

func TestConsoleCapacityRejectsWholeInvalidSelectionBeforeClosing(t *testing.T) {
	sessions := &capacityTestSessions{}
	runtime := testRuntime(t, &sessions.fakeSessions)
	runtime.Sessions = sessions
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	for _, body := range []string{
		`{"sessions":[{"id":1,"generation":1,"last_activity":"2026-11-08T11:00:00Z"}]}`,
		`{"idle_minutes":null,"sessions":[{"id":1,"generation":1,"last_activity":"2026-11-08T11:00:00Z"}]}`,
		`{"idle_minutes":11,"sessions":[{"id":1,"generation":1,"last_activity":"2026-11-08T11:00:00Z"}]}`,
		`{"idle_minutes":30,"sessions":[{"id":1,"generation":1,"last_activity":"2026-11-08T11:00:00Z"},{"id":1,"generation":1,"last_activity":"2026-11-08T11:00:00Z"}]}`,
		`{"idle_minutes":0,"sessions":[{"id":1,"generation":1,"last_activity":"2026-11-08T11:00:00Z"},{"id":2,"generation":1,"last_activity":"2026-11-08T11:00:00Z"}]}`,
		`{"idle_minutes":30,"sessions":[]}`,
	} {
		response := httptest.NewRecorder()
		handlers.CloseCapacity(response, request("POST", "/api/console/capacity/close", body))
		if response.Code != 400 || len(sessions.closed) > 0 {
			t.Fatalf("%d %v", response.Code, sessions.closed)
		}
	}
	runtime.Principal = func() (Principal, error) {
		return Principal{Kind: PrincipalMCPToken, TokenID: 1, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}, nil
	}
	response := httptest.NewRecorder()
	handlers.CloseCapacity(response, request("POST", "/api/console/capacity/close", `{}`))
	if response.Code != 403 {
		t.Fatal(response.Code)
	}
}
