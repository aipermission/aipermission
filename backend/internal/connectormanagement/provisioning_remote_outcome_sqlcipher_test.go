package connectormanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type remoteUnknownProvisioningConnector struct {
	publicationCleanupConnector
	testing *testing.T
	state   *provisioningHTTPTestState
	calls   *int
	cause   error
}

func (connector remoteUnknownProvisioningConnector) ProvisionCredentialProfile(ctx context.Context, runtime connectors.RuntimeContext, input map[string]any) (connectors.ProvisionedCredentialProfile, error) {
	connector.state.requireExclusive(connector.testing)
	*connector.calls++
	// Even a usable partial profile must not authorize publication or cleanup.
	profile, _ := connector.managementTestConnector.ProvisionCredentialProfile(ctx, runtime, input)
	return profile, connector.cause
}

func TestProvisioningSQLCipherRemoteOutcomeUnknownRefusesPublicationAndCleanup(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	store := connectortargets.NewStore(fixture.database)
	if err := store.SetCredentialProfileEncryptedSecret(t.Context(), fixture.target.ID, fixture.profile.ID, "encrypted-admin-secret"); err != nil {
		t.Fatal(err)
	}
	const secret = "remote-admin-secret-canary"
	decryptCalls := 0
	runtime := managementCredentialRuntimePorts()
	runtime.DecryptSecret = func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
		decryptCalls++
		if profileID != fixture.profile.ID || encrypted != "encrypted-admin-secret" {
			t.Fatalf("unexpected admin decryption: profile=%d encrypted=%q", profileID, encrypted)
		}
		return map[string]any{"password": secret}, nil
	}
	state := &provisioningHTTPTestState{runtime: &runtime}
	provisionCalls, cleanupCalls := 0, 0
	cause := fmt.Errorf("remote provisioning: %w", connectors.ClassifyOutcomeUnknown("transaction_commit", nil,
		errors.New("COMMIT acknowledgement lost for "+secret)))
	registry := connectors.NewRegistry()
	if err := registry.Register(remoteUnknownProvisioningConnector{
		publicationCleanupConnector: publicationCleanupConnector{cleanupCount: &cleanupCalls},
		testing:                     t, state: state, calls: &provisionCalls, cause: cause,
	}); err != nil {
		t.Fatal(err)
	}
	state.registry = registry

	response := performProvisioningRequest(t, fixture, state)
	var body httptransport.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusConflict || body.Code != connectors.ErrorCode(cause) ||
		body.Error != "remote provisioning: COMMIT acknowledgement lost for [REDACTED CREDENTIAL]" ||
		strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), "managed-secret") {
		t.Fatalf("remote uncertainty lost its original code or redacted message: status=%d body=%s", response.Code, response.Body.String())
	}
	if decryptCalls != 1 || provisionCalls != 1 {
		t.Fatalf("handler did not reach remote provisioning: decrypt=%d provision=%d", decryptCalls, provisionCalls)
	}
	if state.transactionCalls != 0 || state.encryptionCalls != 0 || state.ensureCalls != 0 || cleanupCalls != 0 || len(state.auditActions) != 0 {
		t.Fatalf("remote uncertainty dispatched local publication or compensation: transaction=%d encryption=%d surfaces=%d cleanup=%d audits=%v",
			state.transactionCalls, state.encryptionCalls, state.ensureCalls, cleanupCalls, state.auditActions)
	}
	if state.exclusiveAcquires != 1 || state.exclusiveReleases != 1 || state.exclusiveHeld {
		t.Fatalf("exclusive lease leaked: acquires=%d releases=%d held=%t", state.exclusiveAcquires, state.exclusiveReleases, state.exclusiveHeld)
	}
	for query, expected := range map[string]int{
		`SELECT COUNT(*) FROM connector_credential_profiles`: 1,
		`SELECT COUNT(*) FROM connector_runtime_surfaces`:    2,
		`SELECT COUNT(*) FROM audit_outbox`:                  0,
	} {
		var actual int
		if err := fixture.database.QueryRowContext(t.Context(), query).Scan(&actual); err != nil || actual != expected {
			t.Fatalf("remote uncertainty changed local rows: query=%q count=%d want=%d error=%v", query, actual, expected, err)
		}
	}
	if stats := fixture.database.Stats(); stats.InUse != 0 {
		t.Fatalf("handler left a pinned local connection: %+v", stats)
	}
}
