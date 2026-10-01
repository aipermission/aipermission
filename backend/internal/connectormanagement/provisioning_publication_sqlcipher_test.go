package connectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

func TestProvisioningSQLCipherReadbackReconcilesCommittedProfileAndAtomicAudit(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	cleanupCount := 0
	registry := connectors.NewRegistry()
	if err := registry.Register(publicationCleanupConnector{cleanupCount: &cleanupCount}); err != nil {
		t.Fatal(err)
	}
	state := &provisioningHTTPTestState{registry: registry}
	coordinator := observability.NewCoordinator(fixture.database, nil, nil, nil)
	state.transaction = func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
		if err := coordinator.WithTransaction(ctx, func(tx *sql.Tx, appendAudit observability.Appender) error {
			return mutate(tx, AuditAppender(appendAudit))
		}); err != nil {
			return err
		}
		// The local commit happened, but acknowledgement was lost above its owner.
		return transactionstate.UnknownWithSafeReadback(errors.New("lost local publication acknowledgement"))
	}
	response := performProvisioningRequest(t, fixture, state)
	if response.Code != http.StatusCreated || cleanupCount != 0 || strings.Contains(response.Body.String(), "managed-secret") {
		t.Fatalf("committed publication was not safely reconciled: status=%d cleanup=%d body=%s", response.Code, cleanupCount, response.Body.String())
	}
	var audits int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE action = 'connector.profile.provisioned'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("reconciled profile lost or duplicated its atomic audit: %d %v", audits, err)
	}
}

func TestProvisioningSQLCipherAcknowledgedRollbackCompensatesOnlyUnpublishedProfile(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	cleanupCount := 0
	registry := connectors.NewRegistry()
	if err := registry.Register(publicationCleanupConnector{cleanupCount: &cleanupCount}); err != nil {
		t.Fatal(err)
	}
	state := &provisioningHTTPTestState{registry: registry, ensureErr: errors.New("runtime construction failed")}
	coordinator := observability.NewCoordinator(fixture.database, nil, nil, nil)
	state.transaction = func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
		return coordinator.WithTransaction(ctx, func(tx *sql.Tx, appendAudit observability.Appender) error {
			return mutate(tx, AuditAppender(appendAudit))
		})
	}
	response := performProvisioningRequest(t, fixture, state)
	if response.Code != http.StatusInternalServerError || cleanupCount != 1 {
		t.Fatalf("acknowledged rollback did not compensate: status=%d cleanup=%d body=%s", response.Code, cleanupCount, response.Body.String())
	}
	var profiles, audits int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM connector_credential_profiles WHERE label = 'managed'`).Scan(&profiles); err != nil || profiles != 0 {
		t.Fatalf("rolled-back profile survived: %d %v", profiles, err)
	}
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE action = 'connector.profile.provisioned'`).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("rolled-back provisioning audit survived: %d %v", audits, err)
	}
}
