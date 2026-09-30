package management

import (
	"net/http"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
)

func TestCleanupReconciliationRetainsRetiredIdentityWithoutConfirmingReplacement(t *testing.T) {
	for _, change := range []string{"username", "key_material", "removed_profile"} {
		t.Run(change, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator", "secondary")
			gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
			original := seedCleanupReconciliationIntent(t, fixture)
			retireCleanupProfile(t, fixture, server, change)
			fixture.reopen(t)
			snapshot := cleanupStatusForTest(t, fixture, gateway)
			input := cleanupAttestInputForTest(t, snapshot)
			choice := snapshot.Records[0].Choices[0]
			if choice.Identity.Username != "operator" || choice.Identity.KeyDigest != original.Record.Identity.KeyDigest || choice.Identity.Profiles[0].ID != original.Record.Identity.Profiles[0].ID {
				t.Fatalf("retired identity substituted current credential: %#v", choice)
			}
			if response := cleanupAttestForTest(t, fixture, gateway, input); response.StatusCode != http.StatusOK {
				t.Fatalf("retired evidence not reconcilable: %#v", response)
			}
			groups, err := planKeyCleanup(t.Context(), fixture.gateway, fixture.runtime, fixture.target, fixture.runtime.profiles)
			if err != nil {
				t.Fatal(err)
			}
			entry, dispatch, err := keycleanup.New(fixture.runtime.journal).Begin(t.Context(), groups[0].Identity)
			if err != nil || !dispatch || entry.ResourceID == original.ResourceID || entry.Record.Status != keycleanup.Intent {
				t.Fatalf("historical proof confirmed replacement group: %#v dispatch %t error %v", entry, dispatch, err)
			}
			if fixture.runtime.keys.secretReads != 0 || fixture.runtime.journal.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 {
				t.Fatal("retired reconciliation authenticated instead of preserving separate proof")
			}
		})
	}
}

func TestCleanupReconciliationIncludesSharedIdentityAliasesNotUnrelatedHistory(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
	groups, err := planKeyCleanup(t.Context(), fixture.gateway, fixture.runtime, fixture.target, fixture.runtime.profiles)
	if err != nil {
		t.Fatal(err)
	}
	alias := groups[0].Identity
	alias.TargetID = 8
	alias.Profiles = append([]keycleanup.ProfileIdentity(nil), alias.Profiles...)
	alias.Profiles[0].ID = 12
	journal := keycleanup.New(fixture.runtime.journal)
	original, dispatch, err := journal.Begin(t.Context(), alias)
	if err != nil || !dispatch {
		t.Fatalf("alias seed: %#v %t %v", original, dispatch, err)
	}
	unrelated := alias
	unrelated.TargetID, unrelated.Username = 9, "different-operator"
	if _, dispatch, err := journal.Begin(t.Context(), unrelated); err != nil || !dispatch {
		t.Fatalf("independent seed: dispatch %t error %v", dispatch, err)
	}
	snapshot := cleanupStatusForTest(t, fixture, gateway)
	input := cleanupAttestInputForTest(t, snapshot)
	view := snapshot.Records[0]
	if view.Entry.ResourceID != original.ResourceID || view.Entry.Record.Identity.TargetID != 8 || view.Choices[0].Identity.TargetID != fixture.target.ID {
		t.Fatalf("alias hidden or unrelated evidence exposed: %#v", snapshot)
	}
	if response := cleanupAttestForTest(t, fixture, gateway, input); response.StatusCode != http.StatusOK {
		t.Fatalf("current alias cannot reconcile shared historical group: %#v", response)
	}
	entry, dispatch, err := journal.Begin(t.Context(), groups[0].Identity)
	if err != nil || dispatch || entry.Record.Status != keycleanup.Attested {
		t.Fatalf("exact attested alias lost proof: %#v dispatch %t error %v", entry, dispatch, err)
	}
	if fixture.runtime.keys.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 {
		t.Fatal("alias observation or attestation used private credentials")
	}
}
