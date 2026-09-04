package recordcrypto

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"

	_ "github.com/SE-I-T-Digital/go-sqlcipher"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestRewriteLegacyMigratesEveryPersistentRecordTypeOnce(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	workspaceID := "workspace-test"

	for index, recordType := range persistentRecordTypes {
		legacy, err := secretVault.EncryptJSON(map[string]any{"secret": fmt.Sprintf("value-%d", index)})
		if err != nil {
			t.Fatalf("encrypt legacy %s: %v", recordType.Domain, err)
		}
		insertEncryptedRecord(t, database, recordType, int64(index+1), legacy)
	}

	stats, err := RewriteLegacy(context.Background(), database, secretVault, workspaceID)
	if err != nil {
		t.Fatalf("rewrite legacy records: %v", err)
	}
	if stats.Rewritten != len(persistentRecordTypes) || stats.Verified != 0 {
		t.Fatalf("unexpected migration stats: %+v", stats)
	}

	for index, recordType := range persistentRecordTypes {
		encrypted := encryptedRecordValue(t, database, recordType, int64(index+1))
		if !vault.IsRecordEnvelope(encrypted) {
			t.Fatalf("%s was not rewritten as a record envelope", recordType.Domain)
		}
		var decoded map[string]any
		if err := DecryptJSON(secretVault, workspaceID, recordType, int64(index+1), encrypted, &decoded); err != nil {
			t.Fatalf("decrypt rewritten %s: %v", recordType.Domain, err)
		}
		if decoded["secret"] != fmt.Sprintf("value-%d", index) {
			t.Fatalf("unexpected rewritten %s value: %#v", recordType.Domain, decoded)
		}
	}

	stats, err = RewriteLegacy(context.Background(), database, secretVault, workspaceID)
	if err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if stats != (RewriteStats{}) {
		t.Fatalf("completed migration should use the marker fast path: %+v", stats)
	}
}

func TestRewriteLegacyMigratesMixedLegacyAndCurrentRecords(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	workspaceID := "workspace-test"

	legacy, err := secretVault.EncryptJSON(map[string]any{"secret": "legacy"})
	if err != nil {
		t.Fatalf("encrypt legacy record: %v", err)
	}
	current, err := EncryptJSON(secretVault, workspaceID, APIToken, 2, map[string]any{"secret": "current"})
	if err != nil {
		t.Fatalf("encrypt current record: %v", err)
	}
	insertEncryptedRecord(t, database, APIToken, 1, legacy)
	insertEncryptedRecord(t, database, APIToken, 2, current)

	stats, err := RewriteLegacy(context.Background(), database, secretVault, workspaceID)
	if err != nil {
		t.Fatalf("rewrite mixed records: %v", err)
	}
	if stats != (RewriteStats{Rewritten: 1, Verified: 1}) {
		t.Fatalf("unexpected mixed migration stats: %+v", stats)
	}
}

func TestRewriteLegacyUsesKeysetTraversalAcrossManySparseRecords(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	const recordCount = 257
	negativeIDRecord, err := secretVault.EncryptJSON(map[string]any{"secret": "negative-id"})
	if err != nil {
		t.Fatal(err)
	}
	insertEncryptedRecord(t, database, APIToken, math.MinInt64, negativeIDRecord)
	for index := 1; index <= recordCount; index++ {
		legacy, err := secretVault.EncryptJSON(map[string]any{"secret": fmt.Sprintf("value-%d", index)})
		if err != nil {
			t.Fatal(err)
		}
		insertEncryptedRecord(t, database, APIToken, int64(index*3), legacy)
	}

	stats, err := RewriteLegacy(t.Context(), database, secretVault, "workspace-test")
	if err != nil {
		t.Fatal(err)
	}
	if stats != (RewriteStats{Rewritten: recordCount + 1}) {
		t.Fatalf("unexpected migration stats: %+v", stats)
	}
	for _, id := range []int64{math.MinInt64, 3, 300, 771} {
		if encrypted := encryptedRecordValue(t, database, APIToken, id); !vault.IsRecordEnvelope(encrypted) {
			t.Fatalf("record %d was not rewritten", id)
		}
	}
}

func TestRewriteLegacyRejectsWrongStorageBindingOnMarkerFastPath(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	if _, err := RewriteLegacy(t.Context(), database, secretVault, "workspace-one"); err != nil {
		t.Fatalf("initialize binding marker: %v", err)
	}
	if _, err := RewriteLegacy(t.Context(), database, secretVault, "workspace-two"); err == nil {
		t.Fatal("wrong workspace identity passed the authenticated binding sentinel")
	}
}

func TestRewriteLegacyTrustsCompletionMarkerWithoutRescanning(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	if _, err := RewriteLegacy(context.Background(), database, secretVault, "workspace-test"); err != nil {
		t.Fatalf("write completion marker: %v", err)
	}
	legacy, err := secretVault.EncryptJSON(map[string]any{"secret": "late-legacy"})
	if err != nil {
		t.Fatalf("encrypt legacy record: %v", err)
	}
	insertEncryptedRecord(t, database, APIToken, 1, legacy)

	stats, err := RewriteLegacy(context.Background(), database, secretVault, "workspace-test")
	if err != nil {
		t.Fatalf("marker fast path: %v", err)
	}
	if stats != (RewriteStats{}) {
		t.Fatalf("marker fast path stats = %+v", stats)
	}
}

func TestRewriteLegacyRollsBackEveryRecordAndMarkerOnFailure(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	legacy, err := secretVault.EncryptJSON(map[string]any{"secret": "valid"})
	if err != nil {
		t.Fatalf("encrypt legacy record: %v", err)
	}
	insertEncryptedRecord(t, database, APIToken, 1, legacy)
	insertEncryptedRecord(t, database, CommandRequest, 2, "not-an-encrypted-record")

	if _, err := RewriteLegacy(context.Background(), database, secretVault, "workspace-test"); err == nil {
		t.Fatalf("expected corrupt record migration to fail")
	}
	if got := encryptedRecordValue(t, database, APIToken, 1); got != legacy {
		t.Fatalf("valid record was partially rewritten after rollback")
	}
	var markerCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, markerKey).Scan(&markerCount); err != nil {
		t.Fatalf("count migration markers: %v", err)
	}
	if markerCount != 0 {
		t.Fatalf("failed migration wrote its completion marker")
	}
}

func TestRewriteLegacyRollsBackOnStorageFailure(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	first, err := secretVault.EncryptJSON(map[string]any{"secret": "first"})
	if err != nil {
		t.Fatalf("encrypt first legacy record: %v", err)
	}
	second, err := secretVault.EncryptJSON(map[string]any{"secret": "second"})
	if err != nil {
		t.Fatalf("encrypt second legacy record: %v", err)
	}
	insertEncryptedRecord(t, database, APIToken, 1, first)
	insertEncryptedRecord(t, database, APIToken, 2, second)
	if _, err := database.Exec(`CREATE TRIGGER reject_second_token_rewrite
		BEFORE UPDATE OF token_value ON api_tokens
		WHEN OLD.id = 2
		BEGIN SELECT RAISE(ABORT, 'synthetic rewrite failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, err := RewriteLegacy(context.Background(), database, secretVault, "workspace-test"); err == nil {
		t.Fatal("expected storage failure to abort migration")
	}
	if got := encryptedRecordValue(t, database, APIToken, 1); got != first {
		t.Fatal("first record rewrite was not rolled back")
	}
	if got := encryptedRecordValue(t, database, APIToken, 2); got != second {
		t.Fatal("second record changed after failed rewrite")
	}
	var markerCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, markerKey).Scan(&markerCount); err != nil {
		t.Fatalf("count migration markers: %v", err)
	}
	if markerCount != 0 {
		t.Fatal("failed storage rewrite wrote its completion marker")
	}
}

func TestRewriteLegacyRollsBackWhenCompletionMarkerCannotBeStored(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	legacy, err := secretVault.EncryptJSON(map[string]any{"secret": "marker-failure"})
	if err != nil {
		t.Fatalf("encrypt legacy record: %v", err)
	}
	insertEncryptedRecord(t, database, APIToken, 1, legacy)
	if _, err := database.Exec(`CREATE TRIGGER reject_envelope_marker
		BEFORE INSERT ON settings
		WHEN NEW.key = 'encrypted_record_envelope_version'
		BEGIN SELECT RAISE(ABORT, 'synthetic marker failure'); END`); err != nil {
		t.Fatalf("create marker failure trigger: %v", err)
	}

	if _, err := RewriteLegacy(context.Background(), database, secretVault, "workspace-test"); err == nil {
		t.Fatal("expected marker storage failure to abort migration")
	}
	if got := encryptedRecordValue(t, database, APIToken, 1); got != legacy {
		t.Fatal("record rewrite was not rolled back after marker failure")
	}
}

func TestRewriteLegacyRejectsUnknownMigrationMarker(t *testing.T) {
	database := newMigrationDatabase(t)
	secretVault := newTestVault(t)
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, '99', datetime('now'))`, markerKey); err != nil {
		t.Fatalf("insert future marker: %v", err)
	}
	if _, err := RewriteLegacy(context.Background(), database, secretVault, "workspace-test"); err == nil {
		t.Fatalf("expected unknown migration marker to fail")
	}
}

func newMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	statements := []string{
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE connector_credential_profiles (id INTEGER PRIMARY KEY, encrypted_secret_json TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE connector_credential_resources (id INTEGER PRIMARY KEY, encrypted_secret TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE api_tokens (id INTEGER PRIMARY KEY, token_value TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE connector_action_requests (id INTEGER PRIMARY KEY, encrypted_payload_json TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE command_requests (id INTEGER PRIMARY KEY, encrypted_command TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE backup_providers (id INTEGER PRIMARY KEY, encrypted_secret_json TEXT NOT NULL DEFAULT '')`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("create migration table: %v", err)
		}
	}
	return database
}

func newTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	secretVault, err := vault.New("record-migration-test-secret")
	if err != nil {
		t.Fatalf("create test vault: %v", err)
	}
	return secretVault
}

func insertEncryptedRecord(t *testing.T, database *sql.DB, recordType RecordType, id int64, encrypted string) {
	t.Helper()
	query := fmt.Sprintf(`INSERT INTO %s (id, %s) VALUES (?, ?)`, recordType.Table, recordType.Column)
	if _, err := database.Exec(query, id, encrypted); err != nil {
		t.Fatalf("insert %s record: %v", recordType.Domain, err)
	}
}

func encryptedRecordValue(t *testing.T, database *sql.DB, recordType RecordType, id int64) string {
	t.Helper()
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE id = ?`, recordType.Column, recordType.Table)
	var encrypted string
	if err := database.QueryRow(query, id).Scan(&encrypted); err != nil {
		t.Fatalf("read %s record: %v", recordType.Domain, err)
	}
	return encrypted
}
