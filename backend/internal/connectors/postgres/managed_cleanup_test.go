package postgresconnector

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

func TestManagedCleanupTerminalConfirmationNeedsNoRemoteSecretOrSavedProfileID(t *testing.T) {
	for _, profileID := range []int64{0, 4} {
		runtime, profile, store, secrets := managedLifecycleFixture(t)
		profile.ID = profileID
		result, err := New().CleanupProvisionedCredentialProfile(t.Context(), runtime, profile)
		if err != nil || result.Status != connectors.ResultCompleted || store.reads != 2 || secrets.reads != 0 {
			t.Fatalf("terminal cleanup/compensation: status=%s error=%v journal reads=%d secret reads=%d", result.Status, err, store.reads, secrets.reads)
		}
		output, ok := result.Output.(map[string]any)
		if !ok || output["previously_confirmed"] != true || output["ownership_reassigned_to"] != "admin" {
			t.Fatalf("durable completed outcome lost: %#v", result.Output)
		}
	}
}

func TestManagedCleanupRejectsMissingEvidenceOrChangedAuthorityBeforeRemoteAccess(t *testing.T) {
	for name, mutate := range map[string]func(*connectors.RuntimeContext, *connectors.CredentialProfileView){
		"wrong target kind": func(r *connectors.RuntimeContext, _ *connectors.CredentialProfileView) {
			r.Target.ConnectorKind = "other"
		},
		"markerless profile": func(_ *connectors.RuntimeContext, p *connectors.CredentialProfileView) {
			delete(p.Public, "managed_identity")
		},
		"profile target":    func(_ *connectors.RuntimeContext, p *connectors.CredentialProfileView) { p.TargetID++ },
		"profile connector": func(_ *connectors.RuntimeContext, p *connectors.CredentialProfileView) { p.ConnectorKind = "other" },
		"username": func(_ *connectors.RuntimeContext, p *connectors.CredentialProfileView) {
			p.Public["username"] = "other"
		},
		"role name": func(_ *connectors.RuntimeContext, p *connectors.CredentialProfileView) {
			p.Public["managed_role_name"] = "other"
		},
		"admin reference": func(_ *connectors.RuntimeContext, p *connectors.CredentialProfileView) {
			p.Public["managed_admin_profile_id"] = 99
		},
		"project moved":      func(r *connectors.RuntimeContext, _ *connectors.CredentialProfileView) { r.Target.ProjectID++ },
		"admin replaced":     func(r *connectors.RuntimeContext, _ *connectors.CredentialProfileView) { r.Profile.ID++ },
		"capability missing": func(r *connectors.RuntimeContext, _ *connectors.CredentialProfileView) { r.Capabilities = nil },
		"swapped durable OID": func(_ *connectors.RuntimeContext, p *connectors.CredentialProfileView) {
			ref := p.Public["managed_identity"].(rolejournal.Reference)
			ref.RoleOID++
			p.Public["managed_identity"] = ref
		},
	} {
		t.Run(name, func(t *testing.T) {
			runtime, profile, _, secrets := managedLifecycleFixture(t)
			mutate(&runtime, &profile)
			result, err := New().CleanupProvisionedCredentialProfile(t.Context(), runtime, profile)
			if err == nil || result.Status == connectors.ResultCompleted || secrets.reads != 0 {
				t.Fatalf("invalid evidence authorized remote cleanup: %s %v secret reads=%d", result.Status, err, secrets.reads)
			}
		})
	}
}

func TestOrdinaryProfileCleanupDoesNotRequireJournal(t *testing.T) {
	result, err := New().CleanupProvisionedCredentialProfile(t.Context(), connectors.RuntimeContext{
		Target: connectors.TargetView{ConnectorKind: Kind},
	}, connectors.CredentialProfileView{})
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("ordinary profile cleanup: %s %v", result.Status, err)
	}
}
