package connectormanagement

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type secretCompensationConnector struct {
	managementTestConnector
	cancel context.CancelFunc
	check  func(context.Context, connectors.RuntimeContext, bool)
}

func (c secretCompensationConnector) ProvisionCredentialProfile(ctx context.Context, runtime connectors.RuntimeContext, input map[string]any) (connectors.ProvisionedCredentialProfile, error) {
	c.check(ctx, runtime, false)
	result, err := c.managementTestConnector.ProvisionCredentialProfile(ctx, runtime, input)
	c.cancel()
	return result, err
}

func (c secretCompensationConnector) CleanupProvisionedCredentialProfile(ctx context.Context, runtime connectors.RuntimeContext, _ connectors.CredentialProfileView) (connectors.ActionResult, error) {
	c.check(ctx, runtime, true)
	return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
}

func TestProvisioningCancellationCompensatesWithOriginalSecretBeforeLeaseRelease(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	if err := connectortargets.NewStore(fixture.database).SetCredentialProfileEncryptedSecret(t.Context(), fixture.target.ID, fixture.profile.ID, "encrypted-fixture"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	held := false
	provisioned, cleanups, decrypts, acquisitions, releases := 0, 0, 0, 0, 0
	registry := connectors.NewRegistry()
	if err := registry.Register(secretCompensationConnector{
		cancel: cancel,
		check: func(phaseCtx context.Context, runtime connectors.RuntimeContext, cleanup bool) {
			if phaseCtx.Err() != nil || !held || !connectors.DeliveryAdmissionHeld(phaseCtx, managementTestDeliveryAdmission) {
				t.Fatal("credential operation lost live lifecycle admission")
			}
			if runtime.Secrets == nil {
				t.Fatal("credential operation lost original secrets")
			}
			value, err := runtime.Secrets.GetSecret(phaseCtx, "password")
			if err != nil || value != "original-admin-fixture" {
				t.Fatal("credential operation did not retain original admin secret")
			}
			if cleanup {
				deadline, ok := phaseCtx.Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > provisionCompensationTimeout {
					t.Fatal("compensation context is not bounded")
				}
				if ctx.Err() == nil {
					t.Fatal("fixture request was not canceled after provisioning")
				}
				cleanups++
			} else {
				provisioned++
			}
		},
	}); err != nil {
		t.Fatal(err)
	}
	fixture.registry = registry
	ports := managementCredentialRuntimePorts()
	ports.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
		decrypts++
		return map[string]any{"password": "original-admin-fixture"}, nil
	}
	ports.RuntimeContext = func(target connectortargets.Target, profile connectortargets.CredentialProfile, secrets map[string]any, boundary CredentialBoundary) connectors.RuntimeContext {
		return connectorcredentials.Context(target, profile, secrets, boundary, nil)
	}
	acquire := func(context.Context) (func(), error) {
		acquisitions++
		held = true
		return func() {
			if cleanups != 1 {
				t.Fatal("lease released before completed compensation")
			}
			held = false
			releases++
		}, nil
	}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"input":{}}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", strconv.FormatInt(fixture.target.ID, 10))
	request.SetPathValue("profile_id", strconv.FormatInt(fixture.profile.ID, 10))
	response := httptest.NewRecorder()
	cancellationDispatchHandler(fixture, "provision", ports, acquire)(response, request)
	if provisioned != 1 || cleanups != 1 || decrypts != 1 || acquisitions != 1 || releases != 1 || held {
		t.Fatalf("provisioned=%d cleanup=%d decrypt=%d acquires=%d releases=%d held=%t", provisioned, cleanups, decrypts, acquisitions, releases, held)
	}
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "original-admin-fixture") || strings.Contains(response.Body.String(), "managed-secret") {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}
