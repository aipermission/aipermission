package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestTargetErrorPresentationPreservesIdentityAndHidesInternalDetails(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"target", connectortargets.ErrTargetNotFound, http.StatusNotFound, "connector target not found"},
		{"profile", connectortargets.ErrTargetProfileNotFound, http.StatusNotFound, "connector target not found"},
		{"reference", connectortargets.ErrInvalidTargetRef, http.StatusBadRequest, "invalid connector target ref"},
		{"validation", connectortargets.ValidationError("invalid public input"), http.StatusBadRequest, "invalid public input"},
		{"internal", errors.New("private credential database detail"), http.StatusInternalServerError, "internal server error"},
		{"target conflict", connectortargets.ErrTargetUpdateConflict, http.StatusInternalServerError, "internal server error"},
		{"profile conflict", connectortargets.ErrCredentialProfileUpdateConflict, http.StatusInternalServerError, "internal server error"},
		{"cleanup pending", connectortargets.ErrRemoteCleanupPending, http.StatusInternalServerError, "internal server error"},
		{"missing error", nil, http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, err := range []error{scenario.err, fmt.Errorf("private wrapper: %w", scenario.err)} {
				response := httptest.NewRecorder()
				WriteError(response, err)
				if response.Code != scenario.status || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != fmt.Sprintf("{\"error\":%q}\n", scenario.message) {
					t.Fatalf("presentation = %d %s %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
				}
			}
		})
	}
}

func TestManagementTargetErrorPresentationPreservesOriginalHandlerPolicy(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"target", connectortargets.ErrTargetNotFound, http.StatusNotFound, "connector target not found"},
		{"profile", connectortargets.ErrTargetProfileNotFound, http.StatusNotFound, "connector target not found"},
		{"reference", connectortargets.ErrInvalidTargetRef, http.StatusBadRequest, "invalid connector target ref"},
		{"validation", connectortargets.ValidationError("invalid public input"), http.StatusBadRequest, "invalid public input"},
		{"internal", errors.New("private credential database detail"), http.StatusInternalServerError, "internal server error"},
		{"target conflict", connectortargets.ErrTargetUpdateConflict, http.StatusConflict, ""},
		{"profile conflict", connectortargets.ErrCredentialProfileUpdateConflict, http.StatusConflict, ""},
		{"conflict precedence", errors.Join(connectortargets.ErrTargetNotFound, connectortargets.ErrTargetUpdateConflict), http.StatusConflict, ""},
		{"cleanup pending", connectortargets.ErrRemoteCleanupPending, http.StatusInternalServerError, "internal server error"},
		{"missing error", nil, http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, err := range []error{scenario.err, fmt.Errorf("private wrapper: %w", scenario.err)} {
				response := httptest.NewRecorder()
				WriteManagementError(response, err)
				message := scenario.message
				if scenario.status == http.StatusConflict {
					message = err.Error()
				}
				if response.Code != scenario.status || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != fmt.Sprintf("{\"error\":%q}\n", message) {
					t.Fatalf("presentation = %d %s %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
				}
			}
		})
	}
}
