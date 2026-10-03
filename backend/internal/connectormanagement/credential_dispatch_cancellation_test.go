package connectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type cancellationDispatchConnector struct {
	managementTestConnector
	calls *int
}

func (c cancellationDispatchConnector) TestConnection(context.Context, connectors.RuntimeContext) (connectors.TestResult, error) {
	*c.calls++
	return connectors.TestResult{Status: connectors.TestOK}, nil
}

func (c cancellationDispatchConnector) ProvisionCredentialProfile(ctx context.Context, runtime connectors.RuntimeContext, input map[string]any) (connectors.ProvisionedCredentialProfile, error) {
	*c.calls++
	return c.managementTestConnector.ProvisionCredentialProfile(ctx, runtime, input)
}

func (c cancellationDispatchConnector) Backup(context.Context, connectors.RuntimeContext, connectors.BackupRequest) (connectors.BackupArtifact, error) {
	*c.calls++
	return connectors.BackupArtifact{Data: []byte("select 1;")}, nil
}

func (c cancellationDispatchConnector) Restore(context.Context, connectors.RuntimeContext, connectors.RestoreRequest) (connectors.ActionResult, error) {
	*c.calls++
	return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
}

func TestCredentialHandlersStopBeforeDispatchWhenPreparationCancels(t *testing.T) {
	for _, operation := range []string{"test", "backup", "provision"} {
		for _, phase := range []string{"decrypt", "factory"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				fixture := newManagementHTTPFixture(t)
				if err := connectortargets.NewStore(fixture.database).SetCredentialProfileEncryptedSecret(t.Context(), fixture.target.ID, fixture.profile.ID, "encrypted-fixture"); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls, factories, acquisitions, releases := 0, 0, 0, 0
				registry := connectors.NewRegistry()
				if err := registry.Register(cancellationDispatchConnector{calls: &calls}); err != nil {
					t.Fatal(err)
				}
				fixture.registry = registry
				ports := managementCredentialRuntimePorts()
				ports.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
					if phase == "decrypt" {
						cancel()
					}
					return map[string]any{"password": "cancellation-fixture-secret"}, nil
				}
				factory := ports.RuntimeContext
				ports.RuntimeContext = func(target connectortargets.Target, profile connectortargets.CredentialProfile, secret map[string]any, boundary CredentialBoundary) connectors.RuntimeContext {
					factories++
					if phase == "factory" {
						cancel()
					}
					return factory(target, profile, secret, boundary)
				}
				acquire := func(context.Context) (func(), error) {
					acquisitions++
					return func() { releases++ }, nil
				}
				handler := cancellationDispatchHandler(fixture, operation, ports, acquire)
				request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"input":{}}`)).WithContext(ctx)
				request.Header.Set("Content-Type", "application/json")
				request.SetPathValue("id", strconv.FormatInt(fixture.target.ID, 10))
				request.SetPathValue("profile_id", strconv.FormatInt(fixture.profile.ID, 10))
				response := httptest.NewRecorder()
				handler(response, request)
				wantFactories := 1
				if phase == "decrypt" {
					wantFactories = 0
				}
				if calls != 0 || factories != wantFactories || acquisitions != 1 || releases != 1 {
					t.Fatalf("calls=%d factories=%d acquisitions=%d releases=%d", calls, factories, acquisitions, releases)
				}
				if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "cancellation-fixture-secret") {
					t.Fatalf("response=%d %s", response.Code, response.Body.String())
				}
			})
		}
	}
}

func cancellationDispatchHandler(fixture *managementHTTPFixture, operation string, ports CredentialRuntimePorts, acquire func(context.Context) (func(), error)) http.HandlerFunc {
	transaction := func(context.Context, func(*sql.Tx, AuditAppender) error) error {
		return errors.New("unexpected publication after preparation cancellation")
	}
	switch operation {
	case "test":
		return NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
			return ProfileTestingScope{
				Database: fixture.database, Registry: fixture.registry, Runtime: ports,
				AcquireDelivery: acquire, Admission: managementTestDeliveryAdmission,
				SpecialTest: noSpecialProfileTest,
				RedactDetails: func(_ context.Context, details map[string]any, _ CredentialBoundary) (map[string]any, error) {
					return details, nil
				},
			}, true
		}).Test
	case "backup":
		return NewProfileBackupHTTPHandler(func(http.ResponseWriter) (ProfileBackupScope, bool) {
			return ProfileBackupScope{
				Database: fixture.database, Registry: fixture.registry, Runtime: ports,
				AcquireDelivery: acquire, AcquireExclusive: acquire, Admission: managementTestDeliveryAdmission,
				Observe: func(context.Context, string, map[string]any) {}, WithTransaction: transaction,
			}, true
		}).Download
	case "provision":
		return NewProvisioningHTTPHandler(func(http.ResponseWriter) (ProvisioningScope, bool) {
			return ProvisioningScope{
				Database: fixture.database, Registry: fixture.registry, Runtime: ports,
				AcquireExclusive: acquire, Admission: managementTestDeliveryAdmission,
				EncryptSecret:   func(context.Context, int64, json.RawMessage) (string, error) { return "encoded", nil },
				WithTransaction: transaction,
				EnsureRuntimeSurfaces: func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error {
					return nil
				},
				AuditRequired: func(context.Context, string, any) error { return nil },
			}, true
		}).Provision
	default:
		panic("unknown cancellation fixture operation")
	}
}
