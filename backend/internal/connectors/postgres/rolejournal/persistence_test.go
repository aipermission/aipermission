package rolejournal

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorresources"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func openPersistedJournal(t *testing.T, path string) (*sql.DB, *connectorresources.Store) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(path, "role-journal-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("role-journal-fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	return database, connectorresources.NewStore(database, secretVault, "role-journal-fixture-workspace")
}

func TestRoleJournalSQLCipherReopenRetainsEveryLifecycleState(t *testing.T) {
	for _, status := range []Status{ProvisionIntent, Provisioned, CleanupIntent, Cleaned, RolledBack} {
		t.Run(string(status), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "journal.db")
			database, resources := openPersistedJournal(t, path)
			journal := New(resources.Scope("postgres", ResourceKind))
			entry := boundTest(t, journal)
			var err error
			switch status {
			case Provisioned, CleanupIntent, Cleaned:
				entry, err = journal.ConfirmProvision(t.Context(), entry)
				if err != nil {
					t.Fatal(err)
				}
				if status == CleanupIntent || status == Cleaned {
					entry, _, err = journal.BeginCleanup(t.Context(), entry)
					if err != nil {
						t.Fatal(err)
					}
				}
				if status == Cleaned {
					entry, err = journal.ConfirmCleanup(t.Context(), entry)
				}
			case RolledBack:
				entry, err = journal.ConfirmRollback(t.Context(), entry)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			_, reopened := openPersistedJournal(t, path)
			journal = New(reopened.Scope("postgres", ResourceKind))
			if got, err := journal.Get(t.Context(), entry.ResourceID); err != nil || got != entry {
				t.Fatalf("identity/generation/state lost across reopen: %#v %v", got, err)
			}
			alias := testAnchor()
			alias.TargetID++
			_, err = journal.BeginProvision(t.Context(), alias, " My Role ")
			if status == Cleaned || status == RolledBack {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrReconciliationRequired) {
				t.Fatalf("reopen lost unresolved predecessor fence: %v", err)
			}
		})
	}
}

type lostCreateResponse struct {
	resourcecontract.CredentialResourceStore
}

func (store lostCreateResponse) Create(ctx context.Context, input resourcecontract.CreateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if _, err := store.CredentialResourceStore.Create(ctx, input); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	return resourcecontract.CredentialResource{}, errors.New("lost acknowledgement after durable intent")
}

func TestRoleJournalSQLCipherLostAcknowledgementAndScopeIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "journal.db")
	database, resources := openPersistedJournal(t, path)
	store := resources.Scope("postgres", ResourceKind)
	if _, err := New(lostCreateResponse{store}).BeginProvision(t.Context(), testAnchor(), "role"); err == nil {
		t.Fatal("lost acknowledgement authorized mutation")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, resources = openPersistedJournal(t, path)
	journal := New(resources.Scope("postgres", ResourceKind))
	if _, err := journal.BeginProvision(t.Context(), testAnchor(), "role"); !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("durable intent lost: %v", err)
	}
	entries, err := journal.List(t.Context())
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected reopened entries: %#v %v", entries, err)
	}
	entry := entries[0]
	for _, foreign := range []resourcecontract.CredentialResourceStore{resources.Scope("other", ResourceKind), resources.Scope("postgres", "private_key")} {
		if _, err := foreign.Get(t.Context(), entry.ResourceID); !errors.Is(err, resourcecontract.ErrCredentialResourceNotFound) {
			t.Fatalf("cross-scope read: %v", err)
		}
		if _, err := foreign.Create(t.Context(), resourcecontract.CreateCredentialResourceInput{Name: "foreign", ResourceType: "foreign", PublicData: "{", Secret: struct{}{}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE connector_credential_resources SET encrypted_secret = 'unreadable' WHERE id = ?`, entry.ResourceID); err != nil {
		t.Fatal(err)
	}
	// Evidence does not need private key/password decryption, even after reopening.
	if _, err := journal.ConfirmRollback(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	if entries, err := journal.List(t.Context()); err != nil || len(entries) != 1 {
		t.Fatalf("foreign records entered journal: %#v %v", entries, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := journal.BeginProvision(ctx, testAnchor(), "new role"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled persistence accepted: %v", err)
	}
}
