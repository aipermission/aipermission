package connectorresources

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestSQLCipherResourceCreateRetiresFailedCommitBeforeJournalEvidenceRead(t *testing.T) {
	database, scope := newResourceTransactionFixture(t)
	for _, statement := range []string{
		`CREATE TABLE resource_parent (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE resource_child (parent_id INTEGER REFERENCES resource_parent(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER fail_resource_commit AFTER INSERT ON connector_credential_resources
 BEGIN INSERT INTO resource_child (parent_id) VALUES (999); END`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	input := resourcecontract.CreateCredentialResourceInput{
		Name: "intent", ResourceType: "journal.v1", PublicData: "durable intent", Fingerprint: "fixture",
		Secret: map[string]any{"value": "fixture material"},
	}
	created, err := scope.Create(t.Context(), input)
	if err == nil || created.ID != 0 || transactionstate.IsNotCommitted(err) || !transactionstate.IsReadbackSafe(err) {
		t.Fatalf("failed driver COMMIT lost its uncertain boundary: resource=%#v error=%v", created, err)
	}
	listed, err := scope.List(t.Context())
	if err != nil || len(listed) != 0 {
		t.Fatalf("uncommitted resource appeared as journal evidence: %#v %v", listed, err)
	}
	var pending int
	if err := database.QueryRow(`SELECT COUNT(*) FROM resource_child`).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("uncertain physical transaction survived retirement: %d %v", pending, err)
	}
	if _, err := database.Exec(`DROP TRIGGER fail_resource_commit`); err != nil {
		t.Fatal(err)
	}
	created, err = scope.Create(t.Context(), input)
	if err != nil || created.ID < 1 {
		t.Fatalf("discarded connection poisoned the next resource transaction: %#v %v", created, err)
	}
	var secret map[string]any
	if err := scope.GetSecret(t.Context(), created.ID, &secret); err != nil || secret["value"] != "fixture material" {
		t.Fatalf("resource encryption failed after physical connection replacement: %#v %v", secret, err)
	}
}

func TestSQLCipherResourceCreateCancellationAcknowledgesRollback(t *testing.T) {
	database, scope := newResourceTransactionFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := resourcecontract.CreateCredentialResourceInput{
		Name: "canceled intent", ResourceType: "journal.v1", PublicData: "durable intent", Fingerprint: "fixture",
		Secret: cancelResourceSecret{cancel: cancel},
	}
	created, err := scope.Create(ctx, input)
	if created.ID != 0 || !errors.Is(err, context.Canceled) || !transactionstate.IsNotCommitted(err) {
		t.Fatalf("canceled resource creation did not acknowledge rollback: %#v %v", created, err)
	}
	listed, err := scope.List(t.Context())
	if err != nil || len(listed) != 0 {
		t.Fatalf("canceled intent survived rollback: %#v %v", listed, err)
	}
	if stats := database.Stats(); stats.InUse != 0 || stats.OpenConnections != 1 {
		t.Fatalf("acknowledged rollback leaked or retired its usable connection: %+v", stats)
	}
	input.Secret = map[string]any{"value": "fixture material"}
	created, err = scope.Create(t.Context(), input)
	if err != nil || created.ID < 1 {
		t.Fatalf("acknowledged rollback prevented the next resource creation: %#v %v", created, err)
	}
}

type cancelResourceSecret struct{ cancel context.CancelFunc }

func (secret cancelResourceSecret) MarshalJSON() ([]byte, error) {
	// INSERT already ran; cancel before the encrypted-secret UPDATE is admitted.
	secret.cancel()
	return []byte(`{"value":"fixture material"}`), nil
}

func newResourceTransactionFixture(t *testing.T) (*sql.DB, resourcecontract.CredentialResourceStore) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "private", "resources.aipdb"), "ResourcePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close resource transaction fixture: %v", err)
		}
	})
	secretVault, err := vault.New("ResourcePassword123")
	if err != nil {
		t.Fatal(err)
	}
	return database, NewStore(database, secretVault, "resource-transaction-fixture").Scope("fixture", "journal")
}
