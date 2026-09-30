package management

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
)

func TestCleanupReconciliationUsesPublicSnapshotsWithoutAuthentication(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
	initial := cleanupStatusForTest(t, fixture, gateway)
	if len(initial.Records) != 0 || initial.ContextDigest == "" {
		t.Fatalf("empty cleanup status: %#v", initial)
	}
	seedCleanupReconciliationIntent(t, fixture)
	snapshot := cleanupStatusForTest(t, fixture, gateway)
	input := cleanupAttestInputForTest(t, snapshot)
	if initial.ContextDigest != snapshot.ContextDigest {
		t.Fatal("journal observation changed the deletion-context identity")
	}
	path := filepath.Join(server.homes["operator"], ".ssh", "authorized_keys")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	response := cleanupAttestForTest(t, fixture, gateway, input)
	if response.StatusCode != http.StatusOK || fixture.runtime.keys.secretReads != 0 || fixture.runtime.journal.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 || len(gateway.audits) != 1 {
		t.Fatalf("reconciliation authenticated, disclosed or missed audit: %#v audits %#v", response, gateway.audits)
	}
	fixture.reopen(t)
	updated := cleanupStatusForTest(t, fixture, gateway)
	if len(updated.Records) != 1 || updated.Records[0].Entry.Record.Status != keycleanup.Attested || updated.Records[0].Entry.Record.Generation == input.Generation {
		t.Fatalf("reopened attestation missing: %#v", updated)
	}
	stale := cleanupAttestForTest(t, fixture, gateway, input)
	if stale.StatusCode != http.StatusConflict || len(gateway.audits) != 1 {
		t.Fatalf("stale generation was accepted or audited: %#v", stale)
	}
	deleted, err := fixture.delete(t)
	if err != nil || deleted.Code != http.StatusOK || fixture.runtime.keys.secretReads != 0 || server.authAttempts.Load() != 0 {
		t.Fatalf("externally reconciled target reauthenticated: %d %s %v", deleted.Code, deleted.Body.String(), err)
	}
	proof := updated.Records[0].Entry.Record.Attestations[0]
	if !reflect.DeepEqual(gateway.audits[0]["subjects"], proof.Coverage) || gateway.audits[0]["reason"] != proof.Reason || proof.ContextDigest != snapshot.ContextDigest {
		t.Fatal("audit differs from durably confirmed proof")
	}
}

func TestCleanupAttestationRequiresLifecycleExclusionOnlyForMutation(t *testing.T) {
	management := Management{}
	if !management.RequiresTargetOperationExclusion(cleanupAttestOperation) {
		t.Fatal("attestation does not declare lifecycle exclusion")
	}
	for _, operation := range []string{cleanupStatusOperation, "docker-check", "docker-logs", "unknown"} {
		if management.RequiresTargetOperationExclusion(operation) {
			t.Fatalf("observation operation %s unexpectedly requires exclusion", operation)
		}
	}
}
