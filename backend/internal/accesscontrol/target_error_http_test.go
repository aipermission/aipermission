package accesscontrol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestPermissionHTTPPreservesSharedTargetErrorPolicy(t *testing.T) {
	fixture := newHandlerFixture(t)
	registry := connectors.NewRegistry()
	if err := registry.Register(accessTestConnector{}); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"target conflict", connectortargets.ErrTargetUpdateConflict, 409, ""},
		{"profile conflict", connectortargets.ErrCredentialProfileUpdateConflict, 409, ""},
		{"conflict precedence", errors.Join(connectortargets.ErrTargetNotFound, connectortargets.ErrTargetUpdateConflict), 409, ""},
		{"missing target", connectortargets.ErrTargetNotFound, 404, "connector target not found"},
		{"missing profile", connectortargets.ErrTargetProfileNotFound, 404, "connector target not found"},
		{"invalid ref", connectortargets.ErrInvalidTargetRef, 400, "invalid connector target ref"},
		{"validation", connectortargets.ValidationError("invalid permission"), 400, "invalid permission"},
		{"internal", errors.New("private database detail"), 500, "internal server error"},
		{"cleanup pending", connectortargets.ErrRemoteCleanupPending, 500, "internal server error"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			failure := fmt.Errorf("private wrapper: %w", scenario.err)
			handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
				return Scope{
					Database: fixture.database, Tokens: tokens.NewStore(fixture.database), Registry: registry,
					Mutate:                  func(context.Context, string, func() any, func(*sql.Tx) error) error { return failure },
					AcquireExclusive:        func(context.Context) (func(), error) { return func() {}, nil },
					FinishTokenInvalidation: func(context.Context, int64, []int64) { t.Error("failed request invalidated token") },
				}, true
			})
			request := httptest.NewRequest(http.MethodPut, "/permissions", strings.NewReader(`{"permissions":[]}`))
			request.Header.Set("Content-Type", "application/json")
			request.SetPathValue("id", strconv.FormatInt(fixture.tokenID, 10))
			response := httptest.NewRecorder()
			handlers.UpdateConnectorPermissions(response, request)
			message := scenario.message
			if scenario.status == http.StatusConflict {
				message = failure.Error()
			}
			if response.Code != scenario.status || response.Body.String() != fmt.Sprintf("{\"error\":%q}\n", message) {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}
