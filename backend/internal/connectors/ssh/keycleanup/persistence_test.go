package keycleanup

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorresources"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func openPersistedJournal(t *testing.T, path string) (*sql.DB, resourcecontract.CredentialResourceStore) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(path, "test-journal-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("journal-fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	resources := connectorresources.NewStore(database, secretVault, "journal-fixture-workspace")
	return database, resources.Scope("ssh", ResourceKind)
}

func TestJournalPersistsPartialCleanupAcrossEncryptedDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	database, store := openPersistedJournal(t, path)
	journal := New(store)
	first := testIdentity(t)
	second := testIdentity(t)
	second.Username = "secondary"
	firstEntry := beginTest(t, journal, first)
	secondEntry := beginTest(t, journal, second)
	confirmed, err := journal.Confirm(ctx, firstEntry)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedDB, reopened := openPersistedJournal(t, path)
	journal = New(reopened)
	entry, dispatch, err := journal.Begin(ctx, first)
	if err != nil || dispatch || entry.Record.Generation != confirmed.Record.Generation {
		t.Fatalf("confirmed first cleanup not recovered: %#v, %t, %v", entry, dispatch, err)
	}
	if _, dispatch, err := journal.Begin(ctx, second); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("uncertain second cleanup lost its fence: %t, %v", dispatch, err)
	}
	attested, err := journal.Attest(ctx, secondEntry, decisionForTest(t, secondEntry, second, "Verified absent through the independent provider console"))
	if err != nil {
		t.Fatal(err)
	}
	if _, dispatch, err := journal.Begin(ctx, second); err != nil || dispatch || attested.Record.Generation == secondEntry.Record.Generation {
		t.Fatalf("reconciliation not persisted: %t, %v", dispatch, err)
	}
	if err := reopenedDB.Close(); err != nil {
		t.Fatal(err)
	}
	_, reopened = openPersistedJournal(t, path)
	entry, dispatch, err = New(reopened).Begin(ctx, second)
	if err != nil || dispatch || !reflect.DeepEqual(entry, attested) {
		t.Fatalf("attestation state changed across reopen: %#v, %t, %v", entry, dispatch, err)
	}
	if _, err := New(reopened).Attest(ctx, secondEntry, decisionForTest(t, secondEntry, second, "Stale evidence")); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("reopen accepted stale attestation: %v", err)
	}
}

type lostCreateResponse struct {
	resourcecontract.CredentialResourceStore
}

func (store lostCreateResponse) Create(ctx context.Context, input resourcecontract.CreateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if _, err := store.CredentialResourceStore.Create(ctx, input); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	return resourcecontract.CredentialResource{}, errors.New("lost create response after durable commit")
}

func TestJournalReopenRetainsIntentAfterLostStorageResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	database, store := openPersistedJournal(t, path)
	identity := testIdentity(t)
	if _, dispatch, err := New(lostCreateResponse{store}).Begin(context.Background(), identity); err == nil || dispatch {
		t.Fatalf("lost storage response authorized dispatch: %t, %v", dispatch, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, reopened := openPersistedJournal(t, path)
	if _, dispatch, err := New(reopened).Begin(context.Background(), identity); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("reopened journal lost ambiguous intent: %t, %v", dispatch, err)
	}
}

func TestJournalUsesScopedPublicEvidenceWithoutReadingCredentialSecrets(t *testing.T) {
	ctx := context.Background()
	database, store := openPersistedJournal(t, filepath.Join(t.TempDir(), "journal.db"))
	journal := New(store)
	entry := beginTest(t, journal, testIdentity(t))
	var resourceKind, connectorKind, encrypted string
	err := database.QueryRowContext(ctx, `SELECT connector_kind, resource_kind, encrypted_secret FROM connector_credential_resources WHERE id = ?`, entry.ResourceID).
		Scan(&connectorKind, &resourceKind, &encrypted)
	if err != nil || connectorKind != "ssh" || resourceKind != ResourceKind || encrypted == "" {
		t.Fatalf("scoped journal record = %q, %q, %q, %v", connectorKind, resourceKind, encrypted, err)
	}
	secretVault, err := vault.New("journal-fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	resources := connectorresources.NewStore(database, secretVault, "journal-fixture-workspace")
	if _, err := database.ExecContext(ctx, `UPDATE connector_credential_resources SET encrypted_secret = 'intentionally unreadable' WHERE id = ?`, entry.ResourceID); err != nil {
		t.Fatal(err)
	}
	entry, err = journal.Confirm(ctx, entry)
	if err != nil {
		t.Fatalf("confirmation should not decrypt secret payloads: %v", err)
	}
	for _, scope := range []resourcecontract.CredentialResourceStore{resources.Scope("other", ResourceKind), resources.Scope("ssh", "private_key")} {
		if _, err := scope.Get(ctx, entry.ResourceID); !errors.Is(err, resourcecontract.ErrCredentialResourceNotFound) {
			t.Fatalf("cross-scope journal read = %v", err)
		}
		foreign, err := scope.Create(ctx, resourcecontract.CreateCredentialResourceInput{Name: "foreign-corrupt", ResourceType: "foreign", PublicData: "{", Secret: struct{}{}})
		if err != nil {
			t.Fatal(err)
		}
		entries, err := journal.List(ctx)
		if err != nil || len(entries) != 1 || entries[0].ResourceID != entry.ResourceID {
			t.Fatalf("journal listed a foreign resource: %#v, %v", entries, err)
		}
		if _, err := journal.Attest(ctx, entry, decisionForTest(t, entry, entry.Record.Identity, "Verified absence independently")); err != nil {
			t.Fatal(err)
		}
		entries, err = journal.List(ctx)
		if err != nil || len(entries) != 1 {
			t.Fatalf("attested journal = %#v, %v", entries, err)
		}
		entry = entries[0]
		persistedForeign, err := scope.Get(ctx, foreign.ID)
		if err != nil || !reflect.DeepEqual(persistedForeign, foreign) {
			t.Fatalf("journal changed foreign resource: %#v, %v", persistedForeign, err)
		}
	}
	if _, err := journal.Attest(ctx, entry, decisionForTest(t, entry, entry.Record.Identity, "Repeat independent absence verification")); err != nil {
		t.Fatalf("journal should not decrypt secret payloads: %v", err)
	}
}

func TestJournalReopenRetainsHistoricalEndpointAndTrustAttestations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	database, store := openPersistedJournal(t, path)
	journal := New(store)
	original := testIdentity(t)
	intent := beginTest(t, journal, original)
	changed := testIdentity(t)
	changed.Host, changed.HostFingerprints = "new.test", []string{testFingerprint("new")}
	attested, err := journal.Attest(ctx, intent, decisionForTest(t, intent, changed, "Verified absence at all original and changed locations"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, store = openPersistedJournal(t, path)
	journal = New(store)
	alias := testIdentity(t)
	alias.TargetID, alias.Profiles[0].ID, alias.Profiles[0].KeyID = 10, 11, 12
	alias.Host, alias.HostFingerprints = changed.Host, changed.HostFingerprints
	if _, dispatch, err := journal.Begin(ctx, alias); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("reopened historical endpoint alias dispatched: %t, %v", dispatch, err)
	}
	attested, err = journal.Attest(ctx, attested, decisionForTest(t, attested, alias, "Verified absence at all locations for the imported key alias"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, store = openPersistedJournal(t, path)
	journal = New(store)
	entry, dispatch, err := journal.Begin(ctx, alias)
	if err != nil || dispatch || !reflect.DeepEqual(entry, attested) {
		t.Fatalf("reopened latest alias proof = %#v, %t, %v", entry, dispatch, err)
	}
	for _, previous := range []Identity{original, changed} {
		if _, dispatch, err := journal.Begin(ctx, previous); dispatch || !errors.Is(err, ErrReconciliationRequired) {
			t.Fatalf("historical identity lost after repeated reopen: %t, %v", dispatch, err)
		}
	}
}

func TestJournalCanceledStorageAndCorruptPublicEvidenceFailClosed(t *testing.T) {
	database, store := openPersistedJournal(t, filepath.Join(t.TempDir(), "journal.db"))
	journal := New(store)
	identity := testIdentity(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, dispatch, err := journal.Begin(ctx, identity); dispatch || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled storage authorized dispatch: %t, %v", dispatch, err)
	}
	entry := beginTest(t, journal, identity)
	if _, err := database.Exec(`UPDATE connector_credential_resources SET public_data = '{' WHERE id = ?`, entry.ResourceID); err != nil {
		t.Fatal(err)
	}
	if _, dispatch, err := journal.Begin(context.Background(), identity); err == nil || dispatch {
		t.Fatalf("corrupt persisted evidence authorized dispatch: %t, %v", dispatch, err)
	}
}
