package backups_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestProviderLifecycleDefaultsToDisabledAndClearsSecretOnArchive(t *testing.T) {
	database := openStoreDatabase(t)
	store := backups.NewStore(database)
	provider, err := store.CreateProvider(context.Background(), backups.CreateProviderRequest{
		ProviderType: backups.ServiceProviderType,
		Name:         "Self-hosted backups",
		Public:       map[string]any{"base_url": "https://backup.example.com"},
		Encrypted:    "encrypted-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Status != "disabled" {
		t.Fatalf("expected disabled provider, got %q", provider.Status)
	}
	active, err := store.HasActiveProvider(context.Background())
	if err != nil || active {
		t.Fatalf("unexpected active provider: active=%v err=%v", active, err)
	}
	provider, err = store.UpdateProvider(context.Background(), provider.ID, backups.UpdateProviderRequest{
		Name:   provider.Name,
		Status: "active",
		Public: provider.Public,
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err = store.HasActiveProvider(context.Background())
	if err != nil || !active {
		t.Fatalf("expected active provider: active=%v err=%v", active, err)
	}
	if err := store.ArchiveProvider(context.Background(), provider.ID); err != nil {
		t.Fatal(err)
	}
	var status, encrypted string
	if err := database.QueryRow(`SELECT status, encrypted_secret_json FROM backup_providers WHERE id = ?`, provider.ID).Scan(&status, &encrypted); err != nil {
		t.Fatal(err)
	}
	if status != "archived" || encrypted != "" {
		t.Fatalf("archive did not clear secret: status=%q encrypted=%q", status, encrypted)
	}
}

func TestUpsertRecordKeepsOneLocalRecordPerRemoteVersion(t *testing.T) {
	database := openStoreDatabase(t)
	store := backups.NewStore(database)
	provider, err := store.CreateProvider(context.Background(), backups.CreateProviderRequest{
		ProviderType: backups.ServiceProviderType,
		Name:         "Self-hosted backups",
		Encrypted:    "encrypted-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := backups.CreateRecordRequest{
		ProviderID:      provider.ID,
		DatabaseID:      "database-a",
		DatabaseName:    "Database A",
		ProviderFileID:  "bkp_123",
		Filename:        "database-a.aipdb",
		SizeBytes:       100,
		ChecksumSHA256:  "first",
		BackupCreatedAt: "2026-07-31T10:00:00Z",
		UploadedAt:      "2026-07-31T10:00:00Z",
	}
	first, err := store.UpsertRecord(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.SizeBytes = 120
	request.ChecksumSHA256 = "second"
	second, err := store.UpsertRecord(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.SizeBytes != 120 || second.ChecksumSHA256 != "second" {
		t.Fatalf("unexpected upsert result: first=%#v second=%#v", first, second)
	}
	records, err := store.ListRecords(context.Background(), backups.ListRecordsFilter{ProviderID: provider.ID})
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

func TestMarkMissingProviderRecordsDeletedReconcilesRemotePrune(t *testing.T) {
	database := openStoreDatabase(t)
	store := backups.NewStore(database)
	provider, err := store.CreateProvider(context.Background(), backups.CreateProviderRequest{
		ProviderType: backups.ServiceProviderType,
		Name:         "Self-hosted backups",
		Encrypted:    "encrypted-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"bkp_keep", "bkp_remove"} {
		if _, err := store.UpsertRecord(context.Background(), backups.CreateRecordRequest{
			ProviderID: provider.ID, DatabaseID: "database-a", DatabaseName: "Database A",
			ProviderFileID: id, Filename: id + ".aipdb", SizeBytes: 100,
			BackupCreatedAt: "2026-07-31T10:00:00Z", UploadedAt: "2026-07-31T10:00:00Z",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.MarkMissingProviderRecordsDeleted(context.Background(), provider.ID, []string{"bkp_keep"}); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListRecords(context.Background(), backups.ListRecordsFilter{ProviderID: provider.ID})
	if err != nil || len(records) != 1 || records[0].ProviderFileID != "bkp_keep" {
		t.Fatalf("unexpected reconciled records: %#v err=%v", records, err)
	}
}

func openStoreDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "store.aipdb"), "StrongDatabasePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}
