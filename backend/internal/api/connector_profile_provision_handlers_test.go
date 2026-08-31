package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const provisioningFailureTestConnectorKind = "provisiontest"

type provisioningFailureTestConnector struct {
	localActionTestConnector
	cleanupCalls       atomic.Int32
	cleanupHadDeadline atomic.Bool
	cleanupStatus      connectors.ResultStatus
	cleanupErr         error
	provisionedSecret  any
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
	secret := c.provisionedSecret
	if secret == nil {
		secret = make(chan int)
	}
	return connectors.ProvisionedCredentialProfile{
		Kind:   "managed",
		Label:  "generated-profile",
		Public: map[string]any{"username": "generated-user"},
		Secret: map[string]any{"payload": secret},
		Result: connectors.ActionResult{Status: connectors.ResultCompleted},
	}, nil
}

func (c *provisioningFailureTestConnector) CleanupProvisionedCredentialProfile(ctx context.Context, _ connectors.RuntimeContext, _ connectors.CredentialProfileView) (connectors.ActionResult, error) {
	c.cleanupCalls.Add(1)
	_, hasDeadline := ctx.Deadline()
	c.cleanupHadDeadline.Store(hasDeadline)
	status := c.cleanupStatus
	if status == "" {
		status = connectors.ResultCompleted
	}
	return connectors.ActionResult{Status: status}, c.cleanupErr
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
	encrypted, err := fixture.server.activeRuntime().vault.EncryptJSON(map[string]any{"password": "admin-secret"})
	if err != nil {
		t.Fatalf("encrypt admin secret: %v", err)
	}
	adminProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       target.ConnectorKind,
		Kind:                "admin",
		Label:               "admin",
		EncryptedSecretJSON: encrypted,
	})
	if err != nil {
		t.Fatalf("create admin profile: %v", err)
	}
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
			cleanupErr:      errors.New("remote cleanup fixture failed"),
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
			encrypted, err := fixture.server.activeRuntime().vault.EncryptJSON(map[string]any{"password": "admin-secret"})
			if err != nil {
				t.Fatalf("encrypt admin secret: %v", err)
			}
			adminProfile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
				TargetID:            target.ID,
				ConnectorKind:       target.ConnectorKind,
				Kind:                "admin",
				Label:               "admin",
				EncryptedSecretJSON: encrypted,
			})
			if err != nil {
				t.Fatalf("create admin profile: %v", err)
			}

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
