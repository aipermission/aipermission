package apiadapter

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

const cleanupEvidencePassword = "CleanupEvidenceFixturePassword123"

type cleanupEvidenceSQLCipherFixture struct {
	database  *sql.DB
	path      string
	target    connectortargets.Target
	admin     connectortargets.CredentialProfile
	managed   connectortargets.CredentialProfile
	registry  *connectors.Registry
	resources resourcecontract.ScopedResourceRuntime
}

func newCleanupEvidenceSQLCipherFixture(t *testing.T, status rolejournal.Status) *cleanupEvidenceSQLCipherFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private", "cleanup-evidence.aipdb")
	database, err := appdb.OpenEncrypted(path, cleanupEvidencePassword)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &cleanupEvidenceSQLCipherFixture{database: database, path: path, registry: connectors.NewRegistry()}
	t.Cleanup(func() { _ = fixture.database.Close() })
	if err := fixture.registry.Register(postgresconnector.New()); err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(database)
	fixture.target, err = store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: postgresconnector.Kind, Name: "My database", Config: map[string]any{"database": "main", "host": "fixture.invalid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.admin, err = store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: fixture.target.ID, ConnectorKind: postgresconnector.Kind, Kind: "username_password", Label: "Admin",
		Public: map[string]any{"username": "admin"}, EncryptedSecretJSON: "old-admin-ciphertext-must-not-be-read",
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.bindResources(t)
	journal := rolejournal.New(fixture.resources.CredentialResources(rolejournal.ResourceKind))
	anchor, err := rolejournal.Authority(connectors.RuntimeContext{
		Target: connectorcredentials.TargetView(fixture.target, fixture.admin.ID), Profile: connectortargets.CredentialProfileView(fixture.admin),
	})
	if err != nil {
		t.Fatal(err)
	}
	anchor.ClusterID, anchor.DatabaseOID, anchor.SuccessorOID = "7", 8, 9
	// This fixture proves encrypted persistence and local retirement, not remote
	// protocol acknowledgement. Real-service fixtures cover the remote fence.
	entry, err := journal.BeginProvision(t.Context(), anchor, "reader")
	if err == nil {
		entry, err = journal.BindRole(t.Context(), entry, 10)
	}
	if err == nil {
		entry, err = journal.ConfirmProvision(t.Context(), entry)
	}
	if err == nil && status != rolejournal.Provisioned {
		entry, _, err = journal.BeginCleanup(t.Context(), entry)
	}
	if err == nil && status == rolejournal.Cleaned {
		entry, err = journal.ConfirmCleanup(t.Context(), entry)
	}
	if err != nil {
		t.Fatal(err)
	}
	fixture.managed, err = store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: fixture.target.ID, ConnectorKind: postgresconnector.Kind, Kind: "username_password", Label: "Reader",
		Public: map[string]any{"username": "reader", "managed_by_aipermission": true, "managed_role_name": "reader",
			"managed_admin_profile_id": fixture.admin.ID, "managed_identity": entry.Reference()},
		EncryptedSecretJSON: "managed-ciphertext-must-not-be-read",
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *cleanupEvidenceSQLCipherFixture) bindResources(t *testing.T) {
	t.Helper()
	secretVault, err := vault.New("cleanup-evidence-resource-fixture")
	if err != nil {
		t.Fatal(err)
	}
	fixture.resources = connectorruntime.NewScope(postgresconnector.Kind, connectorruntime.Dependencies{
		Resources: connectorruntime.NewResourceScopes(fixture.database, secretVault, "cleanup-evidence-workspace"),
	}).ScopedResourceRuntime()
}

func changeCleanupEvidenceAdmin(t *testing.T, fixture *cleanupEvidenceSQLCipherFixture, mode string) {
	t.Helper()
	store := connectortargets.NewStore(fixture.database)
	if mode == "archived" {
		if err := store.DeleteCredentialProfile(t.Context(), fixture.target.ID, fixture.admin.ID); err != nil {
			t.Fatal(err)
		}
		return
	}
	input := connectortargets.UpdateCredentialProfileInput{
		TargetID: fixture.target.ID, ProfileID: fixture.admin.ID, ConnectorKind: fixture.admin.ConnectorKind,
		Kind: fixture.admin.Kind, Label: fixture.admin.Label, Public: fixture.admin.Public,
	}
	switch mode {
	case "rotated":
		ciphertext := "rotated-admin-ciphertext-must-not-be-read"
		input.EncryptedSecretJSON, input.ExpectedSecretRevision = &ciphertext, &fixture.admin.SecretRevision
	case "renamed":
		input.Public = map[string]any{"username": "replacement_admin"}
	default:
		t.Fatalf("unknown admin mutation %q", mode)
	}
	updated, err := store.UpdateCredentialProfile(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "rotated" && updated.SecretRevision <= fixture.admin.SecretRevision {
		t.Fatal("rotation did not change the administrator secret revision")
	}
	if mode == "renamed" && updated.Public["username"] != "replacement_admin" {
		t.Fatal("administrator identity did not change")
	}
}
