package db

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

const (
	legacySQLCipherFixture         = "testdata/sqlcipher-4.4.2.aipdb"
	legacySQLCipherFixturePassword = "FixturePassword123"
	legacySQLCipherFixtureSHA256   = "5970ba8021680c642a0514a7d3675f09f9ffd142861f8b6c97c5e68511cc896d"
	legacyApplicationFixture       = "testdata/aipermission-schema13-sqlcipher-4.4.2.aipdb"
	legacyApplicationFixtureSHA256 = "f99ce9213896517a77fd0eabb59a349cc1fc29b758fd0f1984a5140b58fb7233"
)

func TestSQLCipher442FixtureCompatibility(t *testing.T) {
	fixture := readSQLCipherFixture(t)
	hash := sha256.Sum256(fixture)
	if actual := hex.EncodeToString(hash[:]); actual != legacySQLCipherFixtureSHA256 {
		t.Fatalf("fixture checksum changed: got %s", actual)
	}

	path := copySQLCipherFixture(t, fixture, "legacy.aipdb")
	database, err := OpenEncryptedForMigration(path, legacySQLCipherFixturePassword)
	if err != nil {
		t.Fatalf("open SQLCipher 4.4.2 fixture: %v", err)
	}
	defer database.Close()

	var runtimeVersion, payload string
	if err := database.QueryRow(`SELECT runtime_version, payload FROM compatibility_fixture WHERE id = 1`).Scan(&runtimeVersion, &payload); err != nil {
		t.Fatalf("read SQLCipher 4.4.2 fixture: %v", err)
	}
	if runtimeVersion != "4.4.2" || payload != "synthetic compatibility fixture" {
		t.Fatalf("unexpected fixture payload: runtime=%q payload=%q", runtimeVersion, payload)
	}
}

func TestSQLCipher442ApplicationFixtureOpensViaProductionPath(t *testing.T) {
	fixture, err := os.ReadFile(legacyApplicationFixture)
	if err != nil {
		t.Fatalf("read application fixture: %v", err)
	}
	hash := sha256.Sum256(fixture)
	if actual := hex.EncodeToString(hash[:]); actual != legacyApplicationFixtureSHA256 {
		t.Fatalf("application fixture checksum changed: got %s", actual)
	}

	path := copySQLCipherFixture(t, fixture, "application.aipdb")
	database, err := OpenEncrypted(path, legacySQLCipherFixturePassword)
	if err != nil {
		t.Fatalf("open SQLCipher 4.4.2 application fixture: %v", err)
	}
	defer database.Close()
	snapshots, err := filepath.Glob(path + ".pre-migration-v13-*.aipdb")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("pre-migration snapshots = %d, want 1", len(snapshots))
	}
	if err := ValidateEncrypted(snapshots[0], legacySQLCipherFixturePassword); err != nil {
		t.Fatalf("validate encrypted pre-migration snapshot: %v", err)
	}

	var value string
	if err := database.QueryRow(`SELECT value FROM settings WHERE key = 'sqlcipher_compatibility_fixture'`).Scan(&value); err != nil {
		t.Fatalf("read application fixture marker: %v", err)
	}
	if value != "synthetic schema 13 fixture" {
		t.Fatalf("unexpected application fixture marker %q", value)
	}
	var migrationCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("read application fixture migrations: %v", err)
	}
	if migrationCount != currentSchemaVersion {
		t.Fatalf("application fixture has %d migrations, want %d", migrationCount, currentSchemaVersion)
	}
}

func TestSQLCipher442FixtureRejectsWrongPasswordAndCorruption(t *testing.T) {
	fixture := readSQLCipherFixture(t)
	path := copySQLCipherFixture(t, fixture, "wrong-password.aipdb")
	if database, err := OpenEncryptedForMigration(path, "WrongFixturePassword"); err == nil {
		_ = database.Close()
		t.Fatal("SQLCipher 4.4.2 fixture opened with the wrong password")
	}

	corrupted := append([]byte(nil), fixture...)
	corrupted[100] ^= 0xff
	path = copySQLCipherFixture(t, corrupted, "corrupted.aipdb")
	if database, err := OpenEncryptedForMigration(path, legacySQLCipherFixturePassword); err == nil {
		_ = database.Close()
		t.Fatal("corrupted SQLCipher 4.4.2 fixture opened successfully")
	}
}

func TestSQLCipher442FixtureSnapshotRekeyAndRename(t *testing.T) {
	fixture := readSQLCipherFixture(t)
	path := copySQLCipherFixture(t, fixture, "source.aipdb")
	database, err := OpenEncryptedForMigration(path, legacySQLCipherFixturePassword)
	if err != nil {
		t.Fatal(err)
	}

	snapshotPath := filepath.Join(t.TempDir(), "snapshot.aipdb")
	if err := Snapshot(database, snapshotPath); err != nil {
		t.Fatalf("snapshot SQLCipher 4.4.2 fixture: %v", err)
	}
	if err := Rekey(database, "ReplacementFixturePassword123"); err != nil {
		t.Fatalf("rekey SQLCipher 4.4.2 fixture: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close rekeyed fixture: %v", err)
	}

	if err := ValidateEncrypted(snapshotPath, legacySQLCipherFixturePassword); err != nil {
		t.Fatalf("validate fixture snapshot: %v", err)
	}
	if err := ValidateEncrypted(path, legacySQLCipherFixturePassword); err == nil {
		t.Fatal("old fixture password remained valid after rekey")
	}
	if err := ValidateEncrypted(path, "ReplacementFixturePassword123"); err != nil {
		t.Fatalf("validate rekeyed fixture: %v", err)
	}

	renamedPath := filepath.Join(filepath.Dir(path), "renamed.aipdb")
	if err := os.Rename(path, renamedPath); err != nil {
		t.Fatalf("rename closed fixture: %v", err)
	}
	if err := ValidateEncrypted(renamedPath, "ReplacementFixturePassword123"); err != nil {
		t.Fatalf("validate renamed fixture: %v", err)
	}
}

func TestSQLCipherWALSurvivesCleanReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.aipdb")
	database, err := OpenEncryptedForMigration(path, "WALFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE wal_fixture (value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create WAL fixture: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO wal_fixture (value) VALUES ('durable')`); err != nil {
		t.Fatalf("write WAL fixture: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close WAL fixture: %v", err)
	}

	reopened, err := OpenEncryptedForMigration(path, "WALFixturePassword123")
	if err != nil {
		t.Fatalf("reopen WAL fixture: %v", err)
	}
	defer reopened.Close()
	var value string
	if err := reopened.QueryRow(`SELECT value FROM wal_fixture`).Scan(&value); err != nil {
		t.Fatalf("read reopened WAL fixture: %v", err)
	}
	if value != "durable" {
		t.Fatalf("unexpected WAL fixture value %q", value)
	}
}

func readSQLCipherFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile(legacySQLCipherFixture)
	if err != nil {
		t.Fatalf("read SQLCipher fixture: %v", err)
	}
	return fixture
}

func copySQLCipherFixture(t *testing.T, fixture []byte, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("copy SQLCipher fixture: %v", err)
	}
	return path
}
