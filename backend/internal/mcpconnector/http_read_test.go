package mcpconnector

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestReadHandlersFailClosedWithoutCompositionScope(t *testing.T) {
	handlers := NewHTTPHandlers(func(http.ResponseWriter, *http.Request) (Scope, bool) {
		return Scope{}, true
	})
	for name, handler := range map[string]http.HandlerFunc{
		"targets": handlers.ListTargets,
		"help":    handlers.GetHelp,
		"actions": handlers.GetActions,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestReadHandlersKeepManagementConflictsPrivate(t *testing.T) {
	for _, cause := range []error{connectortargets.ErrTargetUpdateConflict, connectortargets.ErrCredentialProfileUpdateConflict, connectortargets.ErrRemoteCleanupPending} {
		scope := Scope{
			Database: &sql.DB{}, Registry: connectors.NewRegistry(), TokenID: 1,
			Permissions: func(context.Context) ([]Permission, error) {
				return nil, errors.Join(cause, errors.New("private diagnostic"))
			},
			MetadataEnabled: func(context.Context) (bool, error) { return false, nil },
			Metadata:        func(connectors.TargetView, connectors.CredentialProfileView) map[string]any { return nil },
		}
		handlers := NewHTTPHandlers(func(http.ResponseWriter, *http.Request) (Scope, bool) { return scope, true })
		response := httptest.NewRecorder()
		handlers.ListTargets(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"error\":\"internal server error\"}\n" {
			t.Fatalf("MCP conflict disclosure = %d %s", response.Code, response.Body.String())
		}
	}
}
