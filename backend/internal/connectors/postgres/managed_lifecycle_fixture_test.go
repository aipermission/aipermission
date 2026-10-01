package postgresconnector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type managedCapabilities map[string]connectors.RuntimeCapability

func (values managedCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	return values[name]
}

type recordedRoleStore struct {
	resourcecontract.CredentialResourceStore
	row   resourcecontract.CredentialResource
	reads int
}

func (store *recordedRoleStore) Get(ctx context.Context, id int64) (resourcecontract.CredentialResource, error) {
	store.reads++
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	if id != store.row.ID {
		return resourcecontract.CredentialResource{}, resourcecontract.ErrCredentialResourceNotFound
	}
	return store.row, nil
}

type recordedRoleSecrets struct {
	reads      int
	registered bool
}

func (secrets *recordedRoleSecrets) GetSecret(context.Context, string) (string, error) {
	secrets.reads++
	return "", errors.New("unexpected secret read")
}

func (secrets *recordedRoleSecrets) RegisterSensitiveValue(value string) {
	secrets.registered = value != ""
}

func managedLifecycleFixture(t *testing.T) (connectors.RuntimeContext, connectors.CredentialProfileView, *recordedRoleStore, *recordedRoleSecrets) {
	t.Helper()
	secrets := &recordedRoleSecrets{}
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{ID: 1, ProjectID: 3, ConnectorKind: Kind, Ref: "postgres:1:2",
			Name: "My database", UpdatedAt: "target-revision", Config: map[string]any{"database": "main"}},
		Profile: connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "username_password",
			Label: "Admin", Public: map[string]any{"username": "admin"}, UpdatedAt: "admin-revision", SecretRevision: "admin-secret-revision"},
		Secrets: secrets,
	}
	anchor, err := rolejournal.Authority(runtime)
	if err != nil {
		t.Fatal(err)
	}
	anchor.ClusterID, anchor.DatabaseOID, anchor.SuccessorOID = "18446744073709551615", 10, 11
	entry := rolejournal.Entry{ResourceID: 7, Record: rolejournal.Record{Version: 1, RoleOID: 12,
		Generation: strings.Repeat("a", 32), Status: rolejournal.Cleaned,
		Intent: rolejournal.Intent{Anchor: anchor, RoleName: "reader", OperationID: strings.Repeat("b", 32)}}}
	public, err := json.Marshal(entry.Record)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := json.Marshal(entry.Record.Intent)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(intent)
	store := &recordedRoleStore{row: resourcecontract.CredentialResource{ID: entry.ResourceID,
		Name: "managed-role:" + entry.Record.Intent.OperationID, ResourceType: "managed_role_lifecycle.v1",
		PublicData: string(public), Fingerprint: hex.EncodeToString(sum[:])}}
	runtime.Capabilities = managedCapabilities{rolejournal.CapabilityName: rolejournal.New(store),
		rolejournal.CleanupEvidenceCapabilityName: rolejournal.NewCleanupEvidence(store)}
	profile := connectors.CredentialProfileView{ID: 4, TargetID: 1, ConnectorKind: Kind, Kind: "username_password",
		Public: map[string]any{"username": "reader", "managed_by_aipermission": true, "managed_role_name": "reader",
			"managed_admin_profile_id": int64(2), "managed_identity": entry.Reference()}}
	return runtime, profile, store, secrets
}
