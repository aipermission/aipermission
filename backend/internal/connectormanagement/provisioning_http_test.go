package connectormanagement

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestProvisioningHandlerRejectsIncompleteScope(t *testing.T) {
	handler := NewProvisioningHTTPHandler(func(http.ResponseWriter) (ProvisioningScope, bool) {
		return ProvisioningScope{}, true
	})
	response := httptest.NewRecorder()
	handler.Provision(response, httptest.NewRequest(http.MethodPost, "/api/connector-targets/1/profiles/2/provision", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWriteProvisionErrorPreservesUncertainOutcomeCode(t *testing.T) {
	response := httptest.NewRecorder()
	err := connectors.ClassifyActionError(
		"outcome_unknown", connectors.ResultOutcomeUnknown, nil, errors.New("unsafe source message"),
	)
	writeProvisionError(response, err, "safe reconciled message")
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"code":"outcome_unknown"`, `"error":"safe reconciled message"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("body missing %s: %s", expected, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), "unsafe source message") {
		t.Fatalf("response leaked source error: %s", response.Body.String())
	}
}

func TestProvisioningProfileLabelExistsFailsClosedOnStoreError(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	if err := fixture.database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if exists, err := provisioningProfileLabelExists(t.Context(), connectortargets.NewStore(fixture.database), fixture.target.ID, "generated-profile"); err == nil || exists {
		t.Fatalf("exists=%t err=%v, want a closed-store error", exists, err)
	}
}

func TestRequireCompletedCredentialCleanupRejectsNonTerminalSuccess(t *testing.T) {
	if err := RequireCompletedCredentialCleanup(connectors.ActionResult{Status: connectors.ResultRunning}, nil); err == nil {
		t.Fatal("running cleanup result was accepted")
	}
}

func TestManagedCredentialCleanupRejectsIncompleteRuntime(t *testing.T) {
	if _, err := CleanupProvisionedCredentialProfileIfNeeded(
		t.Context(), ManagedCredentialCleanupScope{}, connectortargets.Target{}, connectortargets.CredentialProfile{},
	); !errors.Is(err, errProfileDeletionRuntimeUnavailable) {
		t.Fatalf("error = %v", err)
	}
}
