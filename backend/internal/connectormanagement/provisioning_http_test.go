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

type failedCleanupProvisioningConnector struct{ managementTestConnector }

type admissionAwareCleanupConnector struct {
	managementTestConnector
	identity *connectors.DeliveryAdmissionIdentity
	called   *bool
}

func (connector admissionAwareCleanupConnector) CleanupProvisionedCredentialProfile(
	ctx context.Context,
	_ connectors.RuntimeContext,
	_ connectors.CredentialProfileView,
) (connectors.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return connectors.ActionResult{}, errors.New("cleanup inherited request cancellation")
	}
	if !connectors.DeliveryAdmissionHeld(ctx, connector.identity) {
		return connectors.ActionResult{}, errors.New("cleanup lost lifecycle admission identity")
	}
	*connector.called = true
	return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
}

type exclusiveProvisioningConnector struct {
	managementTestConnector
	held *bool
}

func (connector exclusiveProvisioningConnector) ProvisionCredentialProfile(
	ctx context.Context,
	runtime connectors.RuntimeContext,
	input map[string]any,
) (connectors.ProvisionedCredentialProfile, error) {
	if connector.held == nil || !*connector.held {
		return connectors.ProvisionedCredentialProfile{}, errors.New("provisioning ran outside the exclusive lifecycle gate")
	}
	return connector.managementTestConnector.ProvisionCredentialProfile(ctx, runtime, input)
}

func (failedCleanupProvisioningConnector) CleanupProvisionedCredentialProfile(
	context.Context,
	connectors.RuntimeContext,
	connectors.CredentialProfileView,
) (connectors.ActionResult, error) {
	return connectors.ActionResult{}, errors.New("remote cleanup failed")
}

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

func TestWriteProvisionFailureCauseMapsStableDomainErrors(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "conflict", err: connectortargets.ErrCredentialProfileUpdateConflict, status: http.StatusConflict},
		{name: "not found", err: connectortargets.ErrTargetNotFound, status: http.StatusNotFound},
		{name: "invalid ref", err: connectortargets.ErrInvalidTargetRef, status: http.StatusBadRequest},
		{name: "validation", err: connectortargets.ValidationError("invalid profile"), status: http.StatusBadRequest},
		{name: "internal", err: errors.New("storage failed"), status: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeProvisionFailureCause(response, testCase.err, "safe message")
			if response.Code != testCase.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
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

func TestProvisioningHandlerPersistsEncryptedProfileAndAudit(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	state := &provisioningHTTPTestState{}
	response := performProvisioningRequest(t, fixture, state)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "managed-secret") {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	if strings.Join(state.auditActions, ",") != "connector.profile.provisioned" || state.ensuredProfileID < 1 {
		t.Fatalf("audit=%v ensured_profile_id=%d", state.auditActions, state.ensuredProfileID)
	}
	profile, err := connectortargets.NewStore(fixture.database).GetCredentialProfile(
		t.Context(), fixture.target.ID, state.ensuredProfileID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Label != "managed" || profile.EncryptedSecretJSON != "encrypted-managed-secret" {
		t.Fatalf("persisted profile=%#v", profile)
	}
}

func TestProvisioningHandlerHoldsExclusiveLifecycleGate(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	state := &provisioningHTTPTestState{}
	registry := connectors.NewRegistry()
	if err := registry.Register(exclusiveProvisioningConnector{held: &state.exclusiveHeld}); err != nil {
		t.Fatal(err)
	}
	state.registry = registry
	response := performProvisioningRequest(t, fixture, state)
	if response.Code != http.StatusCreated {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	if state.exclusiveAcquires != 1 || state.exclusiveHeld || state.exclusiveReleases != 1 {
		t.Fatalf("exclusive acquires=%d releases=%d held=%t", state.exclusiveAcquires, state.exclusiveReleases, state.exclusiveHeld)
	}
}

func TestProvisioningHandlerCompensatesDuplicateProfileWithoutLeakingSecrets(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	createDuplicateProvisioningProfile(t, fixture)
	state := &provisioningHTTPTestState{}
	response := performProvisioningRequest(t, fixture, state)
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "managed-secret") {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	if strings.Join(state.auditActions, ",") != "connector.profile.provisioning_compensated" {
		t.Fatalf("audit=%v", state.auditActions)
	}
	if state.ensuredProfileID != 0 {
		t.Fatalf("duplicate profile reached persistence: %d", state.ensuredProfileID)
	}
}

func TestProvisioningCompensationPreservesAdmissionAfterRequestCancellation(t *testing.T) {
	identity := &connectors.DeliveryAdmissionIdentity{}
	requestCtx, cancelRequest := context.WithCancel(connectors.WithDeliveryAdmission(t.Context(), identity))
	cancelRequest()
	cleanupCalled := false
	auditCalled := false
	connector := admissionAwareCleanupConnector{
		identity: identity,
		called:   &cleanupCalled,
	}
	outcome := compensateProvisioned(
		requestCtx,
		ProvisioningScope{
			Runtime: managementCredentialRuntimePorts(),
			AuditRequired: func(ctx context.Context, _ string, _ any) error {
				if err := ctx.Err(); err != nil {
					t.Fatalf("audit inherited request cancellation: %v", err)
				}
				if !connectors.DeliveryAdmissionHeld(ctx, identity) {
					t.Fatal("audit lost lifecycle admission identity")
				}
				auditCalled = true
				return nil
			},
		},
		connector,
		connectortargets.Target{ID: 4, ConnectorKind: managementTestConnectorKind},
		connectortargets.CredentialProfile{ID: 7, TargetID: 4, ConnectorKind: managementTestConnectorKind},
		map[string]any{},
		connectors.ProvisionedCredentialProfile{Kind: "operator", Label: "managed"},
		"duplicate_profile_label",
		errors.New("duplicate"),
	)
	if outcome.cleanupErr != nil || outcome.auditErr != nil {
		t.Fatalf("compensation outcome = %#v", outcome)
	}
	if !cleanupCalled || !auditCalled {
		t.Fatalf("cleanup=%t audit=%t", cleanupCalled, auditCalled)
	}
}

func TestProvisioningHandlerReportsCompensationFailures(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		connector   connectors.Connector
		auditErr    error
		code        string
		auditAction string
	}{
		{
			name: "remote cleanup failed", connector: failedCleanupProvisioningConnector{},
			code:        "provisioning_reconciliation_required",
			auditAction: "connector.profile.provisioning_reconciliation_required",
		},
		{
			name: "compensation audit failed", connector: managementTestConnector{},
			auditErr: errors.New("audit unavailable"), code: "provisioning_compensation_audit_failed",
			auditAction: "connector.profile.provisioning_compensated",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newManagementHTTPFixture(t)
			createDuplicateProvisioningProfile(t, fixture)
			registry := connectors.NewRegistry()
			if err := registry.Register(testCase.connector); err != nil {
				t.Fatal(err)
			}
			state := &provisioningHTTPTestState{registry: registry, auditErr: testCase.auditErr}
			response := performProvisioningRequest(t, fixture, state)
			if response.Code != http.StatusInternalServerError ||
				!strings.Contains(response.Body.String(), "\""+testCase.code+"\"") {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			if strings.Join(state.auditActions, ",") != testCase.auditAction {
				t.Fatalf("audit=%v", state.auditActions)
			}
		})
	}
}

func createDuplicateProvisioningProfile(t *testing.T, fixture *managementHTTPFixture) {
	t.Helper()
	_, err := connectortargets.NewStore(fixture.database).CreateCredentialProfile(
		t.Context(),
		connectortargets.CreateCredentialProfileInput{
			TargetID: fixture.target.ID, ConnectorKind: fixture.target.ConnectorKind,
			Kind: "operator", Label: "managed",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

type provisioningHTTPTestState struct {
	auditActions      []string
	ensuredProfileID  int64
	registry          *connectors.Registry
	auditErr          error
	exclusiveHeld     bool
	exclusiveAcquires int
	exclusiveReleases int
}

func performProvisioningRequest(
	t *testing.T,
	fixture *managementHTTPFixture,
	state *provisioningHTTPTestState,
) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewProvisioningHTTPHandler(func(http.ResponseWriter) (ProvisioningScope, bool) {
		registry := state.registry
		if registry == nil {
			registry = fixture.registry
		}
		return ProvisioningScope{
			Database: fixture.database, Registry: registry, Runtime: managementCredentialRuntimePorts(),
			AcquireExclusive: func(context.Context) (func(), error) {
				if state.exclusiveHeld {
					t.Fatal("exclusive lifecycle gate acquired twice")
				}
				state.exclusiveAcquires++
				state.exclusiveHeld = true
				return func() {
					state.exclusiveHeld = false
					state.exclusiveReleases++
				}, nil
			},
			EncryptSecret: func(_ context.Context, profileID int64, payload json.RawMessage) (string, error) {
				state.requireExclusive(t)
				if profileID < 1 || !strings.Contains(string(payload), "managed-secret") {
					t.Fatalf("encrypt input profile=%d payload=%s", profileID, payload)
				}
				return "encrypted-managed-secret", nil
			},
			WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
				state.requireExclusive(t)
				tx, err := fixture.database.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				appendAudit := func(_ *sql.Tx, _ string, _ *int64, _ int64, action string, _ any) error {
					state.auditActions = append(state.auditActions, action)
					return nil
				}
				if err := mutate(tx, appendAudit); err != nil {
					return err
				}
				return tx.Commit()
			},
			EnsureRuntimeSurfaces: func(_ context.Context, _ *connectortargets.Store, _ connectortargets.Target, profile connectortargets.CredentialProfile) error {
				state.requireExclusive(t)
				state.ensuredProfileID = profile.ID
				return nil
			},
			AuditRequired: func(_ context.Context, action string, _ any) error {
				state.requireExclusive(t)
				state.auditActions = append(state.auditActions, action)
				return state.auditErr
			},
		}, true
	})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connector-targets/{id}/profiles/{profile_id}/provision", handler.Provision)
	path := "/connector-targets/" + strconv.FormatInt(fixture.target.ID, 10) +
		"/profiles/" + strconv.FormatInt(fixture.profile.ID, 10) + "/provision"
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{\"input\":{}}"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func (state *provisioningHTTPTestState) requireExclusive(t *testing.T) {
	t.Helper()
	if !state.exclusiveHeld {
		t.Fatal("provisioning lifecycle work ran outside the exclusive gate")
	}
}
