package mcpconnector

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type rejectedDeliveryGate struct{ err error }

func (gate rejectedDeliveryGate) Acquire(context.Context) (func(), error) {
	return nil, gate.err
}

func TestOutputDeliveryClassifiesFenceAcquisitionFailure(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "deadline", err: context.DeadlineExceeded, wantStatus: http.StatusRequestTimeout, wantBody: "timed out"},
		{name: "unavailable", err: errors.New("closed"), wantStatus: http.StatusServiceUnavailable, wantBody: "unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorization := &OutputAuthorization{
				Database: &sql.DB{}, Tokens: &tokens.Store{}, Leases: &vaultsessions.Store{},
				Delivery: rejectedDeliveryGate{err: test.err}, MCPStarted: func() bool { return true },
				Principal: func(int64) (executionprincipal.Principal, error) { return executionprincipal.Principal{}, nil },
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/mcp/connector-actions", nil)
			authorization.Deliver(response, request, 1, connectortargets.ActionRequest{}, actions.Response{})
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}
