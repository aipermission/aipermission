package connectormanagement

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

const compositionSecret = "alpha violet-suffix-7291"

type compositionTestConnector struct {
	managementTestConnector
	returnError bool
}

func (c compositionTestConnector) TestConnection(context.Context, connectors.RuntimeContext) (connectors.TestResult, error) {
	message := "password=" + compositionSecret
	if c.returnError {
		return connectors.TestResult{}, errors.New(message)
	}
	return connectors.TestResult{Status: connectors.TestOK, Message: message}, nil
}

func TestConnectionHTTPRedactsCredentialsBeforeOptionalMasking(t *testing.T) {
	for _, returnError := range []bool{false, true} {
		t.Run(strconv.FormatBool(returnError), func(t *testing.T) {
			fixture := newManagementHTTPFixture(t)
			if err := connectortargets.NewStore(fixture.database).SetCredentialProfileEncryptedSecret(
				t.Context(), fixture.target.ID, fixture.profile.ID, "encrypted-fixture",
			); err != nil {
				t.Fatal(err)
			}
			registry := connectors.NewRegistry()
			if err := registry.Register(compositionTestConnector{returnError: returnError}); err != nil {
				t.Fatal(err)
			}
			runtime := managementCredentialRuntimePorts()
			runtime.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
				return map[string]any{"password": compositionSecret}, nil
			}
			runtime.RedactText = func(_ context.Context, value string) string { return securitypolicy.RedactBasic(value) }
			handler := NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
				return ProfileTestingScope{
					Database: fixture.database, Registry: registry, Runtime: runtime,
					AcquireDelivery: managementTestAcquireDelivery, Admission: managementTestDeliveryAdmission,
					SpecialTest: noSpecialProfileTest,
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
			if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "violet-suffix-7291") ||
				!strings.Contains(response.Body.String(), actionresult.CredentialRedactionMarker) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestProvisionFailureRedactsCredentialsBeforeBasicMasking(t *testing.T) {
	boundary := actionresult.NewCredentialBoundary(map[string]any{"password": compositionSecret})
	got := safeProvisionError(boundary, errors.New("password="+compositionSecret))
	if got != "password="+actionresult.CredentialRedactionMarker {
		t.Fatalf("provisioning failure = %q", got)
	}
}
