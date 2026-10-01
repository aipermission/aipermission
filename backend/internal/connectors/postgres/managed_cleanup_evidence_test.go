package postgresconnector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func TestCompletedCleanupEvidenceNeedsOnlyCurrentTargetAndConfirmedJournal(t *testing.T) {
	runtime, profile, store, secrets := managedLifecycleFixture(t)
	evidence := connectors.CleanupEvidenceContext{Target: runtime.Target, Capabilities: runtime.Capabilities}
	// No admin profile, principal, secret accessor or transport can be supplied to
	// this interface. Changing/deleting the original admin cannot block this read.
	result, err := New().ReadCompletedCredentialCleanup(t.Context(), evidence, profile)
	if err != nil || result == nil || result.Status != connectors.ResultCompleted ||
		result.Error != "" || result.Handles != (connectors.ActionHandles{}) || store.reads != 2 || secrets.reads != 0 {
		t.Fatalf("terminal evidence: result=%#v error=%v reads=%d secrets=%d", result, err, store.reads, secrets.reads)
	}
	output, ok := result.Output.(map[string]any)
	if !ok || output["previously_confirmed"] != true || output["dropped"] != true || output["ownership_reassigned_to"] != "admin" {
		t.Fatalf("confirmation lost: %#v", result.Output)
	}
}

func TestCompletedCleanupEvidenceDoesNotPromoteUnresolvedOrRolledBackRecords(t *testing.T) {
	for _, status := range []rolejournal.Status{rolejournal.ProvisionIntent, rolejournal.Provisioned, rolejournal.CleanupIntent, rolejournal.RolledBack} {
		t.Run(string(status), func(t *testing.T) {
			runtime, profile, store, _ := managedLifecycleFixture(t)
			var record rolejournal.Record
			if err := json.Unmarshal([]byte(store.row.PublicData), &record); err != nil {
				t.Fatal(err)
			}
			record.Status = status
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			store.row.PublicData = string(data)
			result, err := New().ReadCompletedCredentialCleanup(t.Context(), connectors.CleanupEvidenceContext{
				Target: runtime.Target, Capabilities: runtime.Capabilities,
			}, profile)
			if err != nil || result != nil || store.reads != 1 {
				t.Fatalf("unresolved record promoted: result=%#v error=%v reads=%d", result, err, store.reads)
			}
		})
	}
}

func TestCompletedCleanupEvidenceRejectsChangedTargetProfileAndMissingJournal(t *testing.T) {
	for name, mutate := range map[string]func(*connectors.CleanupEvidenceContext, *connectors.CredentialProfileView, *recordedRoleStore){
		"target changed": func(e *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, _ *recordedRoleStore) {
			e.Target.ID++
		},
		"project moved": func(e *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, _ *recordedRoleStore) {
			e.Target.ProjectID++
		},
		"revision": func(e *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, _ *recordedRoleStore) {
			e.Target.UpdatedAt += "changed"
		},
		"endpoint": func(e *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, _ *recordedRoleStore) {
			e.Target.Config["host"] = "other"
		},
		"connector": func(e *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, _ *recordedRoleStore) {
			e.Target.ConnectorKind = "other"
		},
		"profile target": func(_ *connectors.CleanupEvidenceContext, p *connectors.CredentialProfileView, _ *recordedRoleStore) {
			p.TargetID++
		},
		"profile kind": func(_ *connectors.CleanupEvidenceContext, p *connectors.CredentialProfileView, _ *recordedRoleStore) {
			p.ConnectorKind = "other"
		},
		"profile name": func(_ *connectors.CleanupEvidenceContext, p *connectors.CredentialProfileView, _ *recordedRoleStore) {
			p.Public["username"] = "other"
		},
		"role name": func(_ *connectors.CleanupEvidenceContext, p *connectors.CredentialProfileView, _ *recordedRoleStore) {
			p.Public["managed_role_name"] = "other"
		},
		"admin reference": func(_ *connectors.CleanupEvidenceContext, p *connectors.CredentialProfileView, _ *recordedRoleStore) {
			p.Public["managed_admin_profile_id"] = int64(99)
		},
		"missing reference": func(_ *connectors.CleanupEvidenceContext, p *connectors.CredentialProfileView, _ *recordedRoleStore) {
			delete(p.Public, "managed_identity")
		},
		"missing capability": func(e *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, _ *recordedRoleStore) {
			e.Capabilities = nil
		},
		"missing record": func(_ *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, s *recordedRoleStore) {
			s.row.ID++
		},
		"invalid record": func(_ *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, s *recordedRoleStore) {
			s.row.PublicData = "{}"
		},
		"fingerprint": func(_ *connectors.CleanupEvidenceContext, _ *connectors.CredentialProfileView, s *recordedRoleStore) {
			s.row.Fingerprint = strings.Repeat("c", 64)
		},
		"swapped OID": func(_ *connectors.CleanupEvidenceContext, p *connectors.CredentialProfileView, _ *recordedRoleStore) {
			ref := p.Public["managed_identity"].(rolejournal.Reference)
			ref.RoleOID++
			p.Public["managed_identity"] = ref
		},
	} {
		t.Run(name, func(t *testing.T) {
			runtime, profile, store, secrets := managedLifecycleFixture(t)
			evidence := connectors.CleanupEvidenceContext{Target: runtime.Target, Capabilities: runtime.Capabilities}
			mutate(&evidence, &profile, store)
			result, err := New().ReadCompletedCredentialCleanup(t.Context(), evidence, profile)
			if err == nil || result != nil || secrets.reads != 0 {
				t.Fatalf("invalid evidence returned confirmation: %#v %v", result, err)
			}
		})
	}
}

type changingCleanupEvidenceStore struct {
	*recordedRoleStore
	second func(context.Context, resourcecontract.CredentialResource) (resourcecontract.CredentialResource, error)
}

func (store changingCleanupEvidenceStore) Get(ctx context.Context, id int64) (resourcecontract.CredentialResource, error) {
	row, err := store.recordedRoleStore.Get(ctx, id)
	if err != nil || store.reads != 2 {
		return row, err
	}
	return store.second(ctx, row)
}

func TestCompletedCleanupEvidenceRequiresFreshTerminalReadback(t *testing.T) {
	for _, scenario := range []string{"generation changed", "record unavailable", "canceled after read", "status changed"} {
		t.Run(scenario, func(t *testing.T) {
			runtime, profile, store, _ := managedLifecycleFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			changed := changingCleanupEvidenceStore{store, func(_ context.Context, row resourcecontract.CredentialResource) (resourcecontract.CredentialResource, error) {
				if scenario == "record unavailable" {
					return resourcecontract.CredentialResource{}, errors.New("readback unavailable")
				}
				if scenario == "canceled after read" {
					cancel()
					return row, nil
				}
				var record rolejournal.Record
				if err := json.Unmarshal([]byte(row.PublicData), &record); err != nil {
					t.Fatal(err)
				}
				if scenario == "generation changed" {
					record.Generation = strings.Repeat("c", 32)
				} else {
					record.Status = rolejournal.CleanupIntent
				}
				data, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				row.PublicData = string(data)
				return row, nil
			}}
			result, err := New().ReadCompletedCredentialCleanup(ctx, connectors.CleanupEvidenceContext{
				Target: runtime.Target, Capabilities: managedCapabilities{rolejournal.CleanupEvidenceCapabilityName: rolejournal.NewCleanupEvidence(changed)},
			}, profile)
			if err == nil || result != nil || store.reads != 2 {
				t.Fatalf("stale/readback evidence accepted: %#v %v reads=%d", result, err, store.reads)
			}
		})
	}
}

func TestCompletedCleanupEvidenceRejectsCanceledAndNilContexts(t *testing.T) {
	runtime, profile, store, _ := managedLifecycleFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, input := range []context.Context{nil, ctx} {
		result, err := New().ReadCompletedCredentialCleanup(input, connectors.CleanupEvidenceContext{Target: runtime.Target, Capabilities: runtime.Capabilities}, profile)
		if err == nil || result != nil || store.reads != 0 {
			t.Fatalf("invalid context read journal: %#v %v reads=%d", result, err, store.reads)
		}
	}
	result, err := New().ReadCompletedCredentialCleanup(t.Context(), connectors.CleanupEvidenceContext{}, connectors.CredentialProfileView{})
	if err != nil || result != nil {
		t.Fatalf("unmanaged profile should fall back: %#v %v", result, err)
	}
}
