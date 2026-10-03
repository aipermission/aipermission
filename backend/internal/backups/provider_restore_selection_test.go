package backups

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestRestoreSelectionRevalidatesProviderAndRecordIdentity(t *testing.T) {
	tests := []struct {
		name, statement string
		current         bool
	}{
		{"unchanged", "SELECT 1", true},
		{"poll timestamp", "UPDATE backup_providers SET last_checked_at = '2026-10-03T00:00:00Z'", true},
		{"disabled", "UPDATE backup_providers SET status = 'disabled'", false},
		{"type", "UPDATE backup_providers SET provider_type = 'other'", false},
		{"configuration", "UPDATE backup_providers SET public_json = '{}'", false},
		{"credential", "UPDATE backup_providers SET encrypted_secret_json = 'changed'", false},
		{"provider deleted", "DELETE FROM backup_providers", false},
		{"record deleted", "UPDATE backup_records SET deleted_at = '2026-10-03T00:00:00Z'", false},
		{"record version", "UPDATE backup_records SET provider_file_id = 'other'", false},
		{"record size", "UPDATE backup_records SET size_bytes = 8", false},
		{"record checksum", "UPDATE backup_records SET checksum_sha256 = 'changed'", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, path := openProviderTestDatabase(t)
			provider := createProviderTestRecord(t, database, "https://backup.example.com", testOldServiceToken, "active")
			record, err := NewStore(database).UpsertRecord(t.Context(), CreateRecordRequest{
				ProviderID: provider.ID, DatabaseID: "db-test", DatabaseName: "Test Database",
				ProviderFileID: "version", Filename: "database.aipdb", SizeBytes: 7, ChecksumSHA256: "checksum",
				BackupCreatedAt: "2026-10-03T00:00:00Z", UploadedAt: "2026-10-03T00:00:00Z",
			})
			if err != nil {
				t.Fatal(err)
			}
			selection, err := SelectProviderRestore(t.Context(), providerTestScope(database, path), provider.ID, record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(t.Context(), test.statement); err != nil {
				t.Fatal(err)
			}
			current, err := selection.Current(t.Context(), database)
			if err != nil || current != test.current {
				t.Fatalf("selection current=%t expected=%t error=%v", current, test.current, err)
			}
		})
	}
	if current, err := (ProviderRestoreSelection{}).Current(context.Background(), (*sql.DB)(nil)); current || !errors.Is(err, ErrIncompleteScope) {
		t.Fatalf("missing database: current=%t err=%v", current, err)
	}
}
