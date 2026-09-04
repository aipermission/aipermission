package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

const provisioningFailureTestConnectorKind = "provisiontest"

type provisioningFailureTestConnector struct {
	localActionTestConnector
	cleanupCalls       atomic.Int32
	cleanupHadDeadline atomic.Bool
	cleanupStatus      connectors.ResultStatus
	cleanupErr         error
	managedAdminID     int64
	provisionedSecret  any
	provisionedResult  connectors.ActionResult
	provisionErr       error
	cleanupOutput      any
}

func (*provisioningFailureTestConnector) Kind() string    { return provisioningFailureTestConnectorKind }
func (*provisioningFailureTestConnector) Label() string   { return "Provisioning test" }
func (*provisioningFailureTestConnector) Version() string { return "0.1" }

func (*provisioningFailureTestConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:  "admin",
			Label: "Admin",
			Schema: connectors.Schema{Fields: []connectors.Field{{
				Name: "password", Type: connectors.FieldSecret, Secret: true,
			}}},
		},
		{
			Kind:  "managed",
			Label: "Managed",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{Name: "username", Type: connectors.FieldString, Required: true},
				{Name: "payload", Type: connectors.FieldJSON, Secret: true, Required: true},
			}},
		},
	}
}

func (c *provisioningFailureTestConnector) ProvisionCredentialProfile(context.Context, connectors.RuntimeContext, map[string]any) (connectors.ProvisionedCredentialProfile, error) {
	if c.provisionErr != nil {
		return connectors.ProvisionedCredentialProfile{}, c.provisionErr
	}
	secret := c.provisionedSecret
	if secret == nil {
		secret = make(chan int)
	}
	return connectors.ProvisionedCredentialProfile{
		Kind:   "managed",
		Label:  "generated-profile",
		Public: map[string]any{"username": "generated-user"},
		Secret: map[string]any{"payload": secret},
		Result: firstProvisionedResult(c.provisionedResult),
	}, nil
}

func firstProvisionedResult(result connectors.ActionResult) connectors.ActionResult {
	if result.Status == "" {
		result.Status = connectors.ResultCompleted
	}
	return result
}

func (c *provisioningFailureTestConnector) CleanupProvisionedCredentialProfile(ctx context.Context, _ connectors.RuntimeContext, _ connectors.CredentialProfileView) (connectors.ActionResult, error) {
	c.cleanupCalls.Add(1)
	_, hasDeadline := ctx.Deadline()
	c.cleanupHadDeadline.Store(hasDeadline)
	status := c.cleanupStatus
	if status == "" {
		status = connectors.ResultCompleted
	}
	return connectors.ActionResult{Status: status, Output: c.cleanupOutput}, c.cleanupErr
}

func (c *provisioningFailureTestConnector) ProvisionedCredentialAdminProfileID(profile connectors.CredentialProfileView) (int64, bool, error) {
	if profile.Kind != "managed" || c.managedAdminID < 1 {
		return 0, false, nil
	}
	return c.managedAdminID, true, nil
}

func (*provisioningFailureTestConnector) PreserveProvisionedCredentialPublic(_ connectors.CredentialProfileView, requested map[string]any) (map[string]any, error) {
	return requested, nil
}

func TestHandleConnectorProvisionErrorPreservesUncertainOutcomeCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	err := connectors.ClassifyActionError(
		"outcome_unknown",
		connectors.ResultOutcomeUnknown,
		map[string]any{"retry_safe": false},
		errors.New("unsafe source message"),
	)
	handleConnectorProvisionError(recorder, err, "safe reconciled message")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, expected := range []string{`"code":"outcome_unknown"`, `"error":"safe reconciled message"`} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Fatalf("response missing %s: %s", expected, recorder.Body.String())
		}
	}
	if strings.Contains(recorder.Body.String(), "unsafe source message") {
		t.Fatalf("response leaked original message: %s", recorder.Body.String())
	}
}

func TestProvisionConnectorCredentialProfileCompensatesPersistenceFailure(t *testing.T) {
	fixture := newAPITestFixture(t)
	connector := &provisioningFailureTestConnector{provisionedSecret: "generated-secret"}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(connector); err != nil {
		t.Fatalf("register provisioning connector: %v", err)
	}

	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: provisioningFailureTestConnectorKind,
		Name:          "provision-target",
		Config:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	adminProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       target.ConnectorKind,
		Kind:                "admin",
		Label:               "admin",
		EncryptedSecretJSON: "",
	})
	if err != nil {
		t.Fatalf("create admin profile: %v", err)
	}
	setProvisionTestProfileSecret(t, fixture.server.activeRuntime(), store, adminProfile, map[string]any{"password": "admin-secret"})
	if _, err := fixture.db.Exec(`
		CREATE TRIGGER reject_generated_profile
		BEFORE INSERT ON connector_credential_profiles
		WHEN NEW.label = 'generated-profile'
		BEGIN
			SELECT RAISE(ABORT, 'forced profile persistence failure');
		END;`); err != nil {
		t.Fatalf("create persistence failure trigger: %v", err)
	}

	path := "/api/connector-targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(adminProfile.ID, 10) + "/provision"
	response := performJSON(fixture.server.Handler(), http.MethodPost, path, "", provisionConnectorCredentialProfileRequest{})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if connector.cleanupCalls.Load() != 1 || !connector.cleanupHadDeadline.Load() {
		t.Fatalf("cleanup calls=%d bounded=%t", connector.cleanupCalls.Load(), connector.cleanupHadDeadline.Load())
	}
	profiles, err := store.ListCredentialProfiles(t.Context(), target.ID)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != adminProfile.ID {
		t.Fatalf("unexpected persisted profiles: %#v", profiles)
	}
	var payload string
	if err := fixture.db.QueryRowContext(t.Context(), `
		SELECT payload_json FROM audit_outbox
		WHERE action = 'connector.profile.provisioning_compensated'
		ORDER BY id DESC LIMIT 1`).Scan(&payload); err != nil {
		t.Fatalf("read compensation audit: %v", err)
	}
	if !strings.Contains(payload, `"failure_stage":"profile_persistence"`) || strings.Contains(payload, "generated-secret") {
		t.Fatalf("unexpected compensation audit payload: %s", payload)
	}
}

func TestProvisionConnectorCredentialProfileRedactsAdminAndGeneratedSecrets(t *testing.T) {
	fixture := newAPITestFixture(t)
	connector := &provisioningFailureTestConnector{
		provisionedSecret: "generated-secret",
		provisionedResult: connectors.ActionResult{
			Status:      connectors.ResultCompleted,
			Output:      map[string]any{"admin_echo": "admin-secret", "generated_echo": "generated-secret"},
			DisplayText: "created with generated-secret",
		},
	}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(connector); err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: provisioningFailureTestConnectorKind, Name: "provision-target", Config: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	adminProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "admin", Label: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	setProvisionTestProfileSecret(t, fixture.server.activeRuntime(), store, adminProfile, map[string]any{"password": "admin-secret"})

	path := "/api/connector-targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(adminProfile.ID, 10) + "/provision"
	response := performJSON(fixture.server.Handler(), http.MethodPost, path, "", provisionConnectorCredentialProfileRequest{})
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); strings.Contains(body, "admin-secret") || strings.Contains(body, "generated-secret") || !strings.Contains(body, connectorCredentialRedactionMarker) {
		t.Fatalf("provision response crossed credential boundary: %s", body)
	}
}

func TestProvisionConnectorCredentialProfileRedactsProvisioningErrors(t *testing.T) {
	fixture := newAPITestFixture(t)
	connector := &provisioningFailureTestConnector{provisionErr: errors.New("remote rejected admin-secret")}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(connector); err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: provisioningFailureTestConnectorKind, Name: "provision-target", Config: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	adminProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "admin", Label: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	setProvisionTestProfileSecret(t, fixture.server.activeRuntime(), store, adminProfile, map[string]any{"password": "admin-secret"})

	path := "/api/connector-targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(adminProfile.ID, 10) + "/provision"
	response := performJSON(fixture.server.Handler(), http.MethodPost, path, "", provisionConnectorCredentialProfileRequest{})
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "admin-secret") || !strings.Contains(response.Body.String(), connectorCredentialRedactionMarker) {
		t.Fatalf("provision error crossed credential boundary: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProvisionConnectorCredentialProfileCompensatesEncryptionFailure(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		cleanupStatus   connectors.ResultStatus
		cleanupErr      error
		wantCode        string
		wantAuditAction string
	}{
		{
			name:            "cleanup completed",
			wantAuditAction: "connector.profile.provisioning_compensated",
		},
		{
			name:            "cleanup could not be confirmed",
			cleanupErr:      errors.New("remote cleanup fixture failed with admin-secret"),
			wantCode:        "provisioning_reconciliation_required",
			wantAuditAction: "connector.profile.provisioning_reconciliation_required",
		},
		{
			name:            "cleanup returned a failed result",
			cleanupStatus:   connectors.ResultFailed,
			wantCode:        "provisioning_reconciliation_required",
			wantAuditAction: "connector.profile.provisioning_reconciliation_required",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAPITestFixture(t)
			connector := &provisioningFailureTestConnector{cleanupStatus: testCase.cleanupStatus, cleanupErr: testCase.cleanupErr}
			if err := fixture.server.activeRuntime().connectorRegistry().Register(connector); err != nil {
				t.Fatalf("register provisioning connector: %v", err)
			}

			store := connectortargets.NewStore(fixture.db)
			target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
				ConnectorKind: provisioningFailureTestConnectorKind,
				Name:          "provision-target",
				Config:        map[string]any{},
			})
			if err != nil {
				t.Fatalf("create target: %v", err)
			}
			adminProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
				TargetID:            target.ID,
				ConnectorKind:       target.ConnectorKind,
				Kind:                "admin",
				Label:               "admin",
				EncryptedSecretJSON: "",
			})
			if err != nil {
				t.Fatalf("create admin profile: %v", err)
			}
			setProvisionTestProfileSecret(t, fixture.server.activeRuntime(), store, adminProfile, map[string]any{"password": "admin-secret"})

			path := "/api/connector-targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(adminProfile.ID, 10) + "/provision"
			response := performJSON(fixture.server.Handler(), http.MethodPost, path, "", provisionConnectorCredentialProfileRequest{})
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if testCase.wantCode == "" {
				if strings.Contains(response.Body.String(), "provisioning_reconciliation_required") {
					t.Fatalf("successful cleanup should preserve the original internal error: %s", response.Body.String())
				}
			} else if !strings.Contains(response.Body.String(), testCase.wantCode) {
				t.Fatalf("response missing code %q: %s", testCase.wantCode, response.Body.String())
			}
			if connector.cleanupCalls.Load() != 1 || !connector.cleanupHadDeadline.Load() {
				t.Fatalf("cleanup calls=%d bounded=%t", connector.cleanupCalls.Load(), connector.cleanupHadDeadline.Load())
			}

			profiles, err := store.ListCredentialProfiles(t.Context(), target.ID)
			if err != nil {
				t.Fatalf("list profiles: %v", err)
			}
			if len(profiles) != 1 || profiles[0].ID != adminProfile.ID {
				t.Fatalf("unexpected persisted profiles: %#v", profiles)
			}

			var payload string
			if err := fixture.db.QueryRowContext(t.Context(), `
				SELECT payload_json FROM audit_outbox
				WHERE action = ? ORDER BY id DESC LIMIT 1`, testCase.wantAuditAction).Scan(&payload); err != nil {
				t.Fatalf("read compensation audit: %v", err)
			}
			if !strings.Contains(payload, `"failure_stage":"secret_encryption"`) || strings.Contains(payload, "admin-secret") {
				t.Fatalf("unexpected compensation audit payload: %s", payload)
			}
		})
	}
}

func TestProfileLabelExistsFailsClosedOnStoreError(t *testing.T) {
	database := openAPITestDB(t)
	store := connectortargets.NewStore(database)
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if exists, err := profileLabelExists(t.Context(), store, 1, "generated-profile"); err == nil || exists {
		t.Fatalf("exists=%t err=%v, want a closed-store error", exists, err)
	}
}

func TestDeleteManagedCredentialProfileRequiresCompletedRemoteCleanup(t *testing.T) {
	fixture := newAPITestFixture(t)
	connector := &provisioningFailureTestConnector{cleanupStatus: connectors.ResultFailed}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(connector); err != nil {
		t.Fatalf("register provisioning connector: %v", err)
	}

	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: provisioningFailureTestConnectorKind,
		Name:          "managed-cleanup-target",
		Config:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	adminProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       target.ConnectorKind,
		Kind:                "admin",
		Label:               "admin",
		EncryptedSecretJSON: "",
	})
	if err != nil {
		t.Fatalf("create admin profile: %v", err)
	}
	setProvisionTestProfileSecret(t, fixture.server.activeRuntime(), store, adminProfile, map[string]any{"password": "admin-secret"})
	connector.managedAdminID = adminProfile.ID
	managedProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: target.ConnectorKind,
		Kind:          "managed",
		Label:         "managed-user",
	})
	if err != nil {
		t.Fatalf("create managed profile: %v", err)
	}

	path := "/api/connector-targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(managedProfile.ID, 10)
	response := performJSON(fixture.server.Handler(), http.MethodDelete, path, "", nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if connector.cleanupCalls.Load() != 1 {
		t.Fatalf("cleanup calls=%d, want 1", connector.cleanupCalls.Load())
	}
	if _, err := store.GetCredentialProfile(t.Context(), target.ID, managedProfile.ID); err != nil {
		t.Fatalf("managed profile should remain active after unconfirmed cleanup: %v", err)
	}
	var deleteAuditCount int
	if err := fixture.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM audit_outbox
		WHERE action = 'connector.profile.deleted'`).Scan(&deleteAuditCount); err != nil {
		t.Fatalf("count delete audit events: %v", err)
	}
	if deleteAuditCount != 0 {
		t.Fatalf("delete audit count=%d, want 0", deleteAuditCount)
	}
}

func setProvisionTestProfileSecret(t *testing.T, runtime *databaseRuntime, store *connectortargets.Store, profile connectortargets.CredentialProfile, secret map[string]any) {
	t.Helper()
	encrypted, err := recordcrypto.EncryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, secret)
	if err != nil {
		t.Fatalf("encrypt profile secret: %v", err)
	}
	if err := store.SetCredentialProfileEncryptedSecret(t.Context(), profile.TargetID, profile.ID, encrypted); err != nil {
		t.Fatalf("store profile secret: %v", err)
	}
}

func TestDeleteManagedCredentialProfileAuditsCompletedExternalCleanup(t *testing.T) {
	fixture := newAPITestFixture(t)
	connector := &provisioningFailureTestConnector{cleanupOutput: map[string]any{
		"role_name": "app_reader", "ownership_reassigned_to": "postgres", "dropped": true,
		"password": "cleanup-secret", "admin_echo": "admin-secret", "managed_echo": "managed-secret",
	}}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(connector); err != nil {
		t.Fatalf("register provisioning connector: %v", err)
	}

	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: provisioningFailureTestConnectorKind, Name: "managed-cleanup-target", Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	adminProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "admin", Label: "admin",
	})
	if err != nil {
		t.Fatalf("create admin profile: %v", err)
	}
	setProvisionTestProfileSecret(t, fixture.server.activeRuntime(), store, adminProfile, map[string]any{"password": "admin-secret"})
	connector.managedAdminID = adminProfile.ID
	managedProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "managed", Label: "managed-user",
	})
	if err != nil {
		t.Fatalf("create managed profile: %v", err)
	}
	setProvisionTestProfileSecret(t, fixture.server.activeRuntime(), store, managedProfile, map[string]any{"payload": "managed-secret"})

	path := "/api/connector-targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(managedProfile.ID, 10)
	response := performJSON(fixture.server.Handler(), http.MethodDelete, path, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := store.GetCredentialProfile(t.Context(), target.ID, managedProfile.ID); err == nil {
		t.Fatal("managed profile should be deleted after confirmed cleanup")
	}
	var payload string
	if err := fixture.db.QueryRowContext(t.Context(), `
		SELECT payload_json FROM audit_outbox
		WHERE action = 'connector.profile.deleted' ORDER BY id DESC LIMIT 1`).Scan(&payload); err != nil {
		t.Fatalf("read delete audit: %v", err)
	}
	for _, expected := range []string{`"external_cleanup"`, `"ownership_reassigned_to":"postgres"`, `"role_name":"app_reader"`} {
		if !strings.Contains(payload, expected) {
			t.Fatalf("delete audit missing %s: %s", expected, payload)
		}
	}
	if strings.Contains(payload, "cleanup-secret") || strings.Contains(payload, "admin-secret") || strings.Contains(payload, "managed-secret") || !strings.Contains(payload, `"password":"[REDACTED]"`) {
		t.Fatalf("delete audit did not redact cleanup output: %s", payload)
	}
}
