package management

import (
	"net/http"
	"os"
	"reflect"
	"slices"
	"testing"
)

func TestCleanupReconciliationRejectsChangedPublicContext(t *testing.T) {
	changes := map[string]func(*testing.T, *cleanupFixture){
		"target_revision":  func(_ *testing.T, f *cleanupFixture) { f.target.UpdatedAt = "target-v2" },
		"target_project":   func(_ *testing.T, f *cleanupFixture) { f.target.ProjectID = 4 },
		"target_name":      func(_ *testing.T, f *cleanupFixture) { f.target.Name = "changed name" },
		"target_config":    func(_ *testing.T, f *cleanupFixture) { f.target.Config["display_hint"] = "changed" },
		"profile_revision": func(_ *testing.T, f *cleanupFixture) { f.runtime.profiles[0].UpdatedAt = "profile-v2" },
		"secret_revision":  func(_ *testing.T, f *cleanupFixture) { f.runtime.profiles[0].SecretRevision = "2" },
		"profile_public":   func(_ *testing.T, f *cleanupFixture) { f.runtime.profiles[0].Public["description"] = "changed" },
		"profile_identity": func(_ *testing.T, f *cleanupFixture) { f.runtime.profiles[0].ID = 7 },
		"username":         func(_ *testing.T, f *cleanupFixture) { f.runtime.profiles[0].Public["username"] = "secondary" },
		"host_trust": func(t *testing.T, f *cleanupFixture) {
			if err := os.WriteFile(f.gateway.path, []byte("malformed host trust\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator", "secondary")
			gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
			original := seedCleanupReconciliationIntent(t, fixture)
			input := cleanupAttestInputForTest(t, cleanupStatusForTest(t, fixture, gateway))
			change(t, fixture)
			response := cleanupAttestForTest(t, fixture, gateway, input)
			entry := cleanupEntryForTest(t, fixture, original.ResourceID)
			if response.StatusCode != http.StatusConflict || !reflect.DeepEqual(entry, original) {
				t.Fatalf("stale context mutated journal: %#v entry %#v", response, entry)
			}
			if len(gateway.audits) != 0 || fixture.runtime.keys.secretReads != 0 || fixture.runtime.journal.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 {
				t.Fatal("stale attestation delivered credentials, dispatched or audited a mutation")
			}
		})
	}
}

func TestCleanupReconciliationContextIgnoresObservationAndProfileOrder(t *testing.T) {
	fixture := newCleanupFixture(t)
	startCleanupSSHServer(t, fixture, "operator")
	second := fixture.runtime.profiles[0]
	second.ID = 3
	fixture.runtime.profiles = append(fixture.runtime.profiles, second)
	gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
	seedCleanupReconciliationIntent(t, fixture)
	before := cleanupStatusForTest(t, fixture, gateway)
	slices.Reverse(fixture.runtime.profiles)
	fixture.target.Status = "observation changed"
	after := cleanupStatusForTest(t, fixture, gateway)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("ephemeral state or profile iteration order changed reconciliation identity")
	}
}
