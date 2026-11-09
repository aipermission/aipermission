package gatewayconnectorapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
)

func TestConsoleLimitHTTPPreservesCountsAndRecoveryHint(t *testing.T) {
	sessions := &fakeSessions{createErr: capacity.LimitError{Used: capacity.Limit, Cause: ErrLiveConsoleSessionLimit}}
	runtime := testRuntime(t, sessions)
	h := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	h.Create(response, request("POST", "/api/console/sessions", `{"runtime_id":1}`))
	var data struct {
		Code        string
		Used, Limit int
		Error       string
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if response.Code != 409 || data.Code != "console_session_limit_reached" || data.Used != 32 || data.Limit != 32 || !strings.Contains(data.Error, "Capacity") {
		t.Fatalf("%d %+v", response.Code, data)
	}
}
