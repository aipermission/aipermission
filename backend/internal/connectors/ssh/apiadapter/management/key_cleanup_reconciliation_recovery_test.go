package management

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
)

func TestCleanupReconciliationLostCommitReplyIsRecoveredByObservation(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_commit", true: "after_commit"}[committed], func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator")
			gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
			original := seedCleanupReconciliationIntent(t, fixture)
			input := cleanupAttestInputForTest(t, cleanupStatusForTest(t, fixture, gateway))
			fixture.runtime.journal.confirmBefore = !committed
			fixture.runtime.journal.confirmAfter = committed
			response := cleanupAttestForTest(t, fixture, gateway, input)
			if response.StatusCode != http.StatusConflict || len(gateway.audits) != 0 {
				t.Fatalf("unconfirmed result reported success: %#v", response)
			}
			fixture.reopen(t)
			observed := cleanupStatusForTest(t, fixture, gateway).Records[0].Entry
			if committed {
				if observed.Record.Status != keycleanup.Attested || observed.Record.Generation == original.Record.Generation || len(observed.Record.Attestations) != 1 {
					t.Fatalf("lost reply concealed durable proof: %#v", observed)
				}
				if replay := cleanupAttestForTest(t, fixture, gateway, input); replay.StatusCode != http.StatusConflict {
					t.Fatalf("stale retry appended a second proof: %#v", replay)
				}
			} else if !reflect.DeepEqual(observed, original) {
				t.Fatalf("failed commit changed proof: %#v", observed)
			}
			if fixture.runtime.keys.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 {
				t.Fatal("recovery used remote authentication instead of local durable observation")
			}
		})
	}
}

func TestCleanupReconciliationAuditFailureDoesNotHideCommittedEvidence(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway, auditErr: errors.New("audit unavailable")}
	seedCleanupReconciliationIntent(t, fixture)
	input := cleanupAttestInputForTest(t, cleanupStatusForTest(t, fixture, gateway))
	response := cleanupAttestForTest(t, fixture, gateway, input)
	if response.StatusCode != http.StatusConflict || len(gateway.audits) != 0 {
		t.Fatalf("audit failure reported success: %#v", response)
	}
	fixture.reopen(t)
	observed := cleanupStatusForTest(t, fixture, gateway).Records[0].Entry
	if observed.Record.Status != keycleanup.Attested || observed.Record.Generation == input.Generation || len(observed.Record.Attestations) != 1 {
		t.Fatalf("audit failure concealed committed decision: %#v", observed)
	}
	gateway.auditErr = nil
	if replay := cleanupAttestForTest(t, fixture, gateway, input); replay.StatusCode != http.StatusConflict || len(gateway.audits) != 0 {
		t.Fatalf("audit failure caused blind mutation replay: %#v", replay)
	}
	if fixture.runtime.keys.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 {
		t.Fatal("audit reconciliation read secrets or dispatched remotely")
	}
}

func TestCleanupReconciliationCanceledWritePreservesIntent(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
	original := seedCleanupReconciliationIntent(t, fixture)
	snapshot := cleanupStatusForTest(t, fixture, gateway)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response, err := attestCleanupSnapshot(ctx, gateway, fixture.runtime, snapshot, cleanupAttestInputForTest(t, snapshot))
	observed := cleanupEntryForTest(t, fixture, original.ResourceID)
	if err != nil || response.StatusCode != http.StatusConflict || !reflect.DeepEqual(original, observed) || len(gateway.audits) != 0 || server.authAttempts.Load() != 0 {
		t.Fatalf("canceled evidence changed state: %#v entry %#v error %v", response, observed, err)
	}
}
