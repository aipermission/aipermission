package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"
	"hash"
	"path/filepath"
	"testing"
)

// These contracts pin the released baseline, not later upgrade migrations.
func TestBaselineMigrationSQLContract(t *testing.T) {
	want := map[int]string{
		1: "b03becea8d950e63549d577926285b2ba23fe668d38f111afa0ef4106e5e5493",
		2: "c0770c089031da63f7379fa95c541b0a614185812efdac9a41a4e39ecbbdf258",
		5: "7b6dafcb5dbcee1df7b4aba206d2046de7c9284c2353b3466d5d1f9aa035367c",
	}
	checked := 0
	for _, migration := range migrations() {
		expected, ok := want[migration.version]
		if !ok {
			continue
		}
		checked++
		if migration.preflight != nil {
			t.Errorf("baseline migration %d unexpectedly has a preflight", migration.version)
		}
		digest := sha256.New()
		writeBaselineContractField(digest, migration.description)
		for _, statement := range migration.statements {
			writeBaselineContractField(digest, statement)
		}
		if got := fmt.Sprintf("%x", digest.Sum(nil)); got != expected {
			t.Errorf("baseline migration %d SQL contract = %s, want %s", migration.version, got, expected)
		}
	}
	if checked != len(want) {
		t.Fatalf("checked %d baseline migrations, want %d", checked, len(want))
	}
}

func TestBaselineMigrationSchemaContract(t *testing.T) {
	database, err := OpenEncryptedForMigration(filepath.Join(t.TempDir(), "baseline.db"), "baseline-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ensureMigrationTable(database); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations() {
		if migration.version > 5 {
			break
		}
		if err := runSingleMigration(database, migration); err != nil {
			t.Fatal(err)
		}
	}
	var applied int
	if err := database.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil || applied != 5 {
		t.Fatalf("applied migrations = %d, error = %v, want 5", applied, err)
	}
	if got := baselineSchemaDigest(t, database); got != "ea1e276a8635b57bb5b97cd0ee25cdfbe21e27862b8c08819cae56903febfac1" {
		t.Fatalf("baseline schema contract = %s", got)
	}
}

func baselineSchemaDigest(t *testing.T, database *sql.DB) string {
	t.Helper()
	rows, err := database.Query("SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master ORDER BY type, name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	digest := sha256.New()
	for rows.Next() {
		var kind, name, table, statement string
		if err := rows.Scan(&kind, &name, &table, &statement); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{kind, name, table, statement} {
			writeBaselineContractField(digest, field)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

func writeBaselineContractField(digest hash.Hash, field string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(field)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write([]byte(field))
}
