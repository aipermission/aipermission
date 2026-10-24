package db

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const uriBoundaryMarker = "synthetic SQLCipher URI boundary marker"

func TestSQLCipherOpenTreatsURIOptionsAsLiteralData(t *testing.T) {
	openers := map[string]func(string, string) (*sql.DB, error){
		"normal":    OpenEncrypted,
		"migration": OpenEncryptedForMigration,
		"import":    OpenEncryptedImportCandidate,
	}
	for name, open := range openers {
		t.Run(name, func(t *testing.T) {
			filename := "literal&hexkey=invalid%26_key=%23.aipdb"
			if runtime.GOOS != "windows" {
				filename = "literal?hexkey=invalid&_key=&mode=memory#.aipdb"
			}
			path := filepath.Join(t.TempDir(), filename)
			password := `Literal"&hexkey=invalid&_key=&mode=memory#';--Password123`
			database, err := open(path, password)
			if err != nil {
				t.Fatal(err)
			}
			writeURIBoundaryMarker(t, database)
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			assertURIBoundaryEncrypted(t, path, password)
		})
	}
}

func TestSQLCipherSnapshotAndRekeyKeepLiteralInputsEncrypted(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "source.aipdb")
	password := `Snapshot"&hexkey=invalid&_key=Password123`
	database, err := OpenEncryptedForMigration(path, password)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	writeURIBoundaryMarker(t, database)
	// A schema-alias-looking filename must remain a bound VACUUM target, not SQL.
	snapshotPath := filepath.Join(directory, "snapshot'; DROP TABLE uri_boundary;--.aipdb")
	if err := Snapshot(database, snapshotPath); err != nil {
		t.Fatal(err)
	}
	assertURIBoundaryEncrypted(t, snapshotPath, password)
	newPassword := `Changed"; DROP TABLE uri_boundary;--&hexkey=invalid!123`
	if err := Rekey(database, newPassword); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	assertURIBoundaryEncrypted(t, path, newPassword)
	if err := ValidateEncrypted(path, password); err == nil {
		t.Fatal("old passphrase still works after rekey")
	}
	// Rekeying the source must not alter the snapshot's original key.
	assertURIBoundaryEncrypted(t, snapshotPath, password)
}

func writeURIBoundaryMarker(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`CREATE TABLE uri_boundary (value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO uri_boundary (value) VALUES (?)`, uriBoundaryMarker); err != nil {
		t.Fatal(err)
	}
}

func assertURIBoundaryEncrypted(t *testing.T, path, password string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) < 16 || bytes.HasPrefix(content, []byte("SQLite format 3\x00")) || bytes.Contains(content, []byte(uriBoundaryMarker)) {
		t.Fatal("expected a nonempty encrypted database without a plaintext header or marker")
	}
	for _, invalidPassword := range []string{"", "WrongBoundaryPassword123"} {
		if opened, err := OpenEncryptedForMigration(path, invalidPassword); err == nil {
			_ = opened.Close()
			t.Fatal("database opened without its exact passphrase")
		}
	}
	reopened, err := OpenEncryptedForMigration(path, password)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var marker string
	if err := reopened.QueryRow(`SELECT value FROM uri_boundary`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != uriBoundaryMarker {
		t.Fatal("literal filename/passphrase round trip lost stored data")
	}
}
