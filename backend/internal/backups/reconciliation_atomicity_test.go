package backups_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/testkit/rowfault"
)

func TestBackupReconciliationRejectsPartialProviderEnumeration(t *testing.T) {
	database := rowfault.Open(func(query string) rowfault.Result {
		if !strings.Contains(query, "FROM backup_records") {
			t.Fatal("unexpected reconciliation query")
		}
		return rowfault.Result{
			Columns: []string{"provider_file_id"}, Rows: [][]driver.Value{{"missing-backup"}},
			IterationError: rowfault.ErrIteration,
		}
	})
	t.Cleanup(func() { _ = database.Close() })
	if err := backups.NewStore(database).MarkMissingProviderRecordsDeleted(t.Context(), 1, nil); !errors.Is(err, rowfault.ErrIteration) {
		t.Fatalf("partial enumeration error = %v", err)
	}
}

func TestBackupReconciliationRollsBackTombstonesAndUploadExpiryTogether(t *testing.T) {
	for _, operation := range []string{"explicit", "missing"} {
		t.Run(operation, func(t *testing.T) {
			database := openStoreDatabase(t)
			store := backups.NewStore(database)
			provider := seedReconciliationAtomicityFixture(t, store)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if _, err := database.Exec(`
				CREATE TRIGGER abort_fixture_upload_expiry
				BEFORE UPDATE OF status ON backup_upload_operations
				WHEN NEW.status = 'expired' AND NEW.provider_file_id = 'remove-b'
				BEGIN SELECT RAISE(ABORT, 'fixture-expiry-abort'); END`); err != nil {
				t.Fatal(err)
			}
			var err error
			if operation == "explicit" {
				err = store.MarkProviderRecordsDeleted(ctx, provider.ID, []string{"remove-a", "remove-b"})
			} else {
				err = store.MarkMissingProviderRecordsDeleted(ctx, provider.ID, nil)
			}
			if err == nil || !strings.Contains(err.Error(), "fixture-expiry-abort") {
				t.Fatalf("expiry failure = %v", err)
			}
			for _, id := range []string{"remove-a", "remove-b"} {
				var deleted, completed sql.NullString
				var status string
				if err := database.QueryRowContext(ctx, `SELECT deleted_at FROM backup_records WHERE provider_id = ? AND provider_file_id = ?`, provider.ID, id).Scan(&deleted); err != nil {
					t.Fatal(err)
				}
				if err := database.QueryRowContext(ctx, `SELECT status, completed_at FROM backup_upload_operations WHERE provider_id = ? AND provider_file_id = ?`, provider.ID, id).Scan(&status, &completed); err != nil {
					t.Fatal(err)
				}
				if deleted.Valid || status != "completed" || !completed.Valid {
					t.Fatalf("partial reconciliation for %s: deleted=%v status=%s completed=%v", id, deleted, status, completed)
				}
			}
		})
	}
}

func seedReconciliationAtomicityFixture(t *testing.T, store *backups.Store) backups.Provider {
	t.Helper()
	provider, err := store.CreateProvider(t.Context(), backups.CreateProviderRequest{
		ProviderType: backups.ServiceProviderType, Name: "Fixture backups", Encrypted: "fixture-encrypted-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"remove-a", "remove-b"} {
		if _, err := store.UpsertRecord(t.Context(), backups.CreateRecordRequest{
			ProviderID: provider.ID, DatabaseID: "fixture-db", DatabaseName: "Fixture database",
			ProviderFileID: id, Filename: id + ".aipdb", SizeBytes: 100,
			BackupCreatedAt: "2026-10-03T10:00:00Z", UploadedAt: "2026-10-03T10:00:00Z",
		}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.ClaimUploadOperation(t.Context(), backups.ClaimUploadOperationRequest{
			IdempotencyKey: id, ProviderID: provider.ID, DatabaseID: "fixture-db",
			WorkspaceInstanceID: "fixture-instance", StreamID: "fixture-stream", SourceInstallationID: "fixture-installation",
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.CompleteUploadOperation(t.Context(), id, id); err != nil {
			t.Fatal(err)
		}
	}
	return provider
}
