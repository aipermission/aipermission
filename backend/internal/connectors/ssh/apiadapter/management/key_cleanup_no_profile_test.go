package management

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
)

func TestCleanupReconciliationSurvivesLastProfileRemovalAndEncryptedReopen(t *testing.T) {
	for _, empty := range [][]connectors.CredentialProfileView{nil, {}} {
		t.Run(map[bool]string{true: "nil", false: "empty"}[empty == nil], func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator")
			gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
			original := seedCleanupReconciliationIntent(t, fixture)
			old := cleanupStatusForTest(t, fixture, gateway)
			fixture.runtime.profiles = empty
			fixture.reopen(t)
			snapshot := cleanupStatusForTest(t, fixture, gateway)
			input := cleanupAttestInputForTest(t, snapshot)
			choice := snapshot.Records[0].Choices[0]
			if !reflect.DeepEqual(choice.Identity, original.Record.Identity) || len(choice.Subjects) == 0 || snapshot.ContextDigest == old.ContextDigest {
				t.Fatalf("historical identity or fresh context lost: %#v", snapshot)
			}
			if response := cleanupAttestForTest(t, fixture, gateway, cleanupAttestInputForTest(t, old)); response.StatusCode != http.StatusConflict {
				t.Fatalf("pre-removal context accepted: %#v", response)
			}
			incomplete := input
			incomplete.Coverage = nil
			if response := cleanupAttestForTest(t, fixture, gateway, incomplete); response.StatusCode != http.StatusConflict {
				t.Fatalf("incomplete historical proof accepted: %#v", response)
			}
			if response := cleanupAttestForTest(t, fixture, gateway, input); response.StatusCode != http.StatusOK {
				t.Fatalf("last-profile evidence not reconcilable: %#v", response)
			}
			fixture.reopen(t)
			entry := cleanupEntryForTest(t, fixture, original.ResourceID)
			if entry.Record.Status != keycleanup.Attested || entry.Record.Generation == original.Record.Generation || len(entry.Record.Attestations) != 1 || len(gateway.audits) != 1 {
				t.Fatalf("attestation not durable/audited: %#v audits=%#v", entry, gateway.audits)
			}
			if response := cleanupAttestForTest(t, fixture, gateway, input); response.StatusCode != http.StatusConflict || len(gateway.audits) != 1 {
				t.Fatalf("retired generation accepted or audited: %#v", response)
			}
			response, err := fixture.delete(t)
			if err != nil || response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "ssh_key_cleanup_preflight_failed") {
				t.Fatalf("remote deletion lost profile preflight: %d %s %v", response.Code, response.Body, err)
			}
			if fixture.runtime.keys.secretReads != 0 || fixture.runtime.journal.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 || gateway.deleteCalls != 0 || gateway.finalizeCalls != 0 {
				t.Fatal("historical evidence granted secret, remote, or deletion authority")
			}
		})
	}
}
