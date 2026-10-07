package mcpconnector

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func TestActionCapacityErrorsDoNotPromiseStorageRecoveryAfterOneMinute(t *testing.T) {
	limits := actioncapacity.DefaultLimits()
	for _, test := range []struct {
		name  string
		usage actioncapacity.Usage
		retry string
	}{
		{"stored_rows", actioncapacity.Usage{Rows: limits.Rows + 1}, ""},
		{"stored_bytes", actioncapacity.Usage{Bytes: limits.Bytes + 1}, ""},
		{"running_requests", actioncapacity.Usage{Running: limits.Running + 1}, "60"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := fmt.Errorf("admission: %w", &connectortargets.ActionRequestCapacityError{Usage: test.usage, Limits: limits})
			response := httptest.NewRecorder()
			writeActionError(response, httptest.NewRequest(http.MethodPost, "/", nil), ActionScope{}, err)
			if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != test.retry {
				t.Fatalf("status=%d headers=%v", response.Code, response.Header())
			}
			if !strings.Contains(response.Body.String(), "connector_action_backpressure") || !strings.Contains(response.Body.String(), "limit="+test.name) {
				t.Fatalf("capacity response=%s", response.Body.String())
			}
		})
	}
}
