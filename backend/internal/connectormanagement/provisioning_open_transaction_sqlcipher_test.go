package connectormanagement

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

func TestProvisioningSQLCipherFailedCommitCannotPublishUncommittedReadback(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	for _, statement := range []string{
		`CREATE TABLE publication_parent (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE publication_child (parent_id INTEGER REFERENCES publication_parent(id) DEFERRABLE INITIALLY DEFERRED)`,
	} {
		if _, err := fixture.database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	cleanupCount := 0
	registry := connectors.NewRegistry()
	if err := registry.Register(publicationCleanupConnector{cleanupCount: &cleanupCount}); err != nil {
		t.Fatal(err)
	}
	state := &provisioningHTTPTestState{registry: registry}
	coordinator := observability.NewCoordinator(fixture.database, nil, nil, nil)
	state.transaction = func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
		return coordinator.WithTransaction(ctx, func(tx *sql.Tx, appendAudit observability.Appender) error {
			if err := mutate(tx, AuditAppender(appendAudit)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO publication_child (parent_id) VALUES (999)`)
			return err
		})
	}
	response := performProvisioningRequest(t, fixture, state)
	if response.Code != http.StatusConflict || cleanupCount != 0 || !strings.Contains(response.Body.String(), "profile_persistence_outcome_unknown") {
		t.Fatalf("failed COMMIT published uncommitted data or destroyed remote credentials: status=%d cleanup=%d body=%s", response.Code, cleanupCount, response.Body.String())
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM connector_credential_profiles WHERE label = 'managed'`,
		`SELECT COUNT(*) FROM audit_outbox WHERE action = 'connector.profile.provisioned'`,
		`SELECT COUNT(*) FROM publication_child`,
	} {
		var pending int
		if err := fixture.database.QueryRow(query).Scan(&pending); err != nil || pending != 0 {
			t.Fatalf("uncertain physical transaction survived discard: %d %v", pending, err)
		}
	}
}

func TestProvisioningSQLCipherFailedSnapshotPreservesKnownProfileID(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	cleanupCount := 0
	registry := connectors.NewRegistry()
	if err := registry.Register(publicationCleanupConnector{cleanupCount: &cleanupCount}); err != nil {
		t.Fatal(err)
	}
	state := &provisioningHTTPTestState{registry: registry}
	state.ensure = func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
		// Make only the final active-profile snapshot fail after its ID is known.
		return store.DeleteCredentialProfile(ctx, target.ID, profile.ID)
	}
	state.transaction = func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
		tx, err := fixture.database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		err = mutate(tx, func(*sql.Tx, string, *int64, int64, string, any) error { return nil })
		if err == nil || state.ensuredProfileID < 1 {
			t.Fatal("fixture did not fail the final snapshot after obtaining an ID")
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		// Simulate losing the acknowledgement above the rollback owner.
		return transactionstate.Unknown(err)
	}
	response := performProvisioningRequest(t, fixture, state)
	if response.Code != http.StatusConflict || cleanupCount != 0 || len(state.auditPayloads) != 1 {
		t.Fatalf("failed snapshot lost uncertain state: status=%d cleanup=%d audits=%d", response.Code, cleanupCount, len(state.auditPayloads))
	}
	payload, ok := state.auditPayloads[0].(map[string]any)
	if !ok || payload["profile_id"] != state.ensuredProfileID || payload["cleanup_status"] != "not_dispatched" {
		t.Fatalf("reconciliation audit lost known identity: %#v", payload)
	}
}
