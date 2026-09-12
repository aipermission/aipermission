package connectormanagement

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type failingConnectionTestConnector struct{ managementTestConnector }

func (failingConnectionTestConnector) TestConnection(context.Context, connectors.RuntimeContext) (connectors.TestResult, error) {
	return connectors.TestResult{}, errors.New("remote reflected connection-secret")
}

func TestProfileTestingHandlerRejectsIncompleteScope(t *testing.T) {
	handler := NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
		return ProfileTestingScope{}, true
	})
	response := httptest.NewRecorder()
	handler.Test(response, httptest.NewRequest(http.MethodPost, "/api/connector-targets/1/profiles/2/test", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProfileTestingHandlerRunsConnectorAndReturnsRedactedResult(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	handler := NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
		return ProfileTestingScope{
			Database: fixture.database, Registry: fixture.registry,
			Runtime: managementCredentialRuntimePorts(),
			SpecialTest: func(http.ResponseWriter, *http.Request, connectors.TargetView, connectors.CredentialProfileView) bool {
				return false
			},
			RedactDetails: func(_ context.Context, details map[string]any, _ CredentialBoundary) (map[string]any, error) {
				return details, nil
			},
		}, true
	})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connector-targets/{id}/profiles/{profile_id}/test", handler.Test)
	path := "/connector-targets/" + strconv.FormatInt(fixture.target.ID, 10) +
		"/profiles/" + strconv.FormatInt(fixture.profile.ID, 10) + "/test"
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		"\"ok\":true", "\"status\":\"ok\"", "\"message\":\"connection ready\"", "\"transport\":\"fixture\"",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("response missing %s: %s", expected, response.Body.String())
		}
	}
}

func TestProfileTestingHandlerRedactsConnectorErrors(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	if err := connectortargets.NewStore(fixture.database).SetCredentialProfileEncryptedSecret(
		t.Context(), fixture.target.ID, fixture.profile.ID, "encrypted-connection-secret",
	); err != nil {
		t.Fatal(err)
	}
	registry := connectors.NewRegistry()
	if err := registry.Register(failingConnectionTestConnector{}); err != nil {
		t.Fatal(err)
	}
	runtime := managementCredentialRuntimePorts()
	runtime.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
		return map[string]any{"password": "connection-secret"}, nil
	}
	handler := NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
		return ProfileTestingScope{
			Database: fixture.database, Registry: registry, Runtime: runtime,
			SpecialTest: func(http.ResponseWriter, *http.Request, connectors.TargetView, connectors.CredentialProfileView) bool {
				return false
			},
			RedactDetails: func(_ context.Context, details map[string]any, _ CredentialBoundary) (map[string]any, error) {
				return details, nil
			},
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.SetPathValue("id", strconv.FormatInt(fixture.target.ID, 10))
	request.SetPathValue("profile_id", strconv.FormatInt(fixture.profile.ID, 10))
	response := httptest.NewRecorder()
	handler.Test(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "\"status\":\"unknown_error\"") {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "connection-secret") {
		t.Fatalf("connection error leaked credential: %s", response.Body.String())
	}
}

func TestProfileTestingHandlerHonorsSpecialTestAndFailsClosedOnDetailRedaction(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	newRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.SetPathValue("id", strconv.FormatInt(fixture.target.ID, 10))
		request.SetPathValue("profile_id", strconv.FormatInt(fixture.profile.ID, 10))
		return request
	}

	special := NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
		return ProfileTestingScope{
			Database: fixture.database, Registry: fixture.registry, Runtime: managementCredentialRuntimePorts(),
			SpecialTest: func(w http.ResponseWriter, _ *http.Request, _ connectors.TargetView, _ connectors.CredentialProfileView) bool {
				w.WriteHeader(http.StatusAccepted)
				return true
			},
			RedactDetails: func(_ context.Context, details map[string]any, _ CredentialBoundary) (map[string]any, error) {
				return details, nil
			},
		}, true
	})
	specialResponse := httptest.NewRecorder()
	special.Test(specialResponse, newRequest())
	if specialResponse.Code != http.StatusAccepted {
		t.Fatalf("special response=%d %s", specialResponse.Code, specialResponse.Body.String())
	}

	redactionFailure := NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
		return ProfileTestingScope{
			Database: fixture.database, Registry: fixture.registry, Runtime: managementCredentialRuntimePorts(),
			SpecialTest: func(http.ResponseWriter, *http.Request, connectors.TargetView, connectors.CredentialProfileView) bool {
				return false
			},
			RedactDetails: func(context.Context, map[string]any, CredentialBoundary) (map[string]any, error) {
				return nil, errors.New("redaction failed")
			},
		}, true
	})
	redactionResponse := httptest.NewRecorder()
	redactionFailure.Test(redactionResponse, newRequest())
	if redactionResponse.Code != http.StatusInternalServerError ||
		strings.Contains(redactionResponse.Body.String(), "redaction failed") {
		t.Fatalf("redaction response=%d %s", redactionResponse.Code, redactionResponse.Body.String())
	}
}

func TestProfileTestingHandlerRejectsMalformedTargetAndProfileIDs(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	handler := NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
		return ProfileTestingScope{
			Database: fixture.database, Registry: fixture.registry, Runtime: managementCredentialRuntimePorts(),
			SpecialTest: func(http.ResponseWriter, *http.Request, connectors.TargetView, connectors.CredentialProfileView) bool {
				return false
			},
			RedactDetails: func(_ context.Context, details map[string]any, _ CredentialBoundary) (map[string]any, error) {
				return details, nil
			},
		}, true
	})
	for _, ids := range [][2]string{{"invalid", "1"}, {"1", "invalid"}} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.SetPathValue("id", ids[0])
		request.SetPathValue("profile_id", ids[1])
		handler.Test(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("ids=%v response=%d %s", ids, response.Code, response.Body.String())
		}
	}
	for _, ids := range [][2]string{
		{"999999", strconv.FormatInt(fixture.profile.ID, 10)},
		{strconv.FormatInt(fixture.target.ID, 10), "999999"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.SetPathValue("id", ids[0])
		request.SetPathValue("profile_id", ids[1])
		handler.Test(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("ids=%v response=%d %s", ids, response.Code, response.Body.String())
		}
	}
}

func managementCredentialRuntimePorts() CredentialRuntimePorts {
	return CredentialRuntimePorts{
		DecryptSecret: func(context.Context, int64, string) (map[string]any, error) {
			return map[string]any{}, nil
		},
		RuntimeContext: func(target connectortargets.Target, profile connectortargets.CredentialProfile, _ map[string]any, _ CredentialBoundary) connectors.RuntimeContext {
			return connectors.RuntimeContext{
				Target: connectors.TargetView{
					ID: target.ID, ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config,
				},
				Profile: connectortargets.CredentialProfileView(profile),
			}
		},
		RedactResult: func(_ context.Context, result connectors.ActionResult, _ CredentialBoundary) (connectors.ActionResult, error) {
			return result, nil
		},
		RedactText: func(_ context.Context, value string) string { return value },
	}
}
