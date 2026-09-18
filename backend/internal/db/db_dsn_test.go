package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenEncryptedKeepsReservedCharactersInDatabasePath(t *testing.T) {
	for _, filename := range []string{"vault?#.db", "vault#?.db", "vault#only.db", "vault%20.db"} {
		t.Run(filename, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), filename)
			database, err := OpenEncryptedForMigration(path, "correct-password")
			if err != nil {
				t.Fatalf("open encrypted database: %v", err)
			}
			defer database.Close()

			var sequence int
			var schema, actualPath string
			if err := database.QueryRow("PRAGMA database_list").Scan(&sequence, &schema, &actualPath); err != nil {
				t.Fatalf("read main database path: %v", err)
			}
			if schema != "main" {
				t.Fatalf("expected main database, got %q", schema)
			}
			expected, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat requested database: %v", err)
			}
			actual, err := os.Stat(actualPath)
			if err != nil {
				t.Fatalf("stat opened database: %v", err)
			}
			if !os.SameFile(expected, actual) {
				t.Fatalf("opened %q instead of %q", actualPath, path)
			}
		})
	}
}

func TestOpenEncryptedKeepsRelativeDatabasePath(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	absolutePath := filepath.Join(t.TempDir(), "relative?.db")
	relativePath, err := filepath.Rel(workingDirectory, absolutePath)
	if err != nil {
		t.Fatal(err)
	}
	database, err := OpenEncryptedForMigration(relativePath, "correct-password")
	if err != nil {
		t.Fatalf("open relative encrypted database: %v", err)
	}
	defer database.Close()

	var sequence int
	var schema, actualPath string
	if err := database.QueryRow("PRAGMA database_list").Scan(&sequence, &schema, &actualPath); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(absolutePath)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.Stat(actualPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(expected, actual) {
		t.Fatalf("opened %q instead of %q", actualPath, absolutePath)
	}
}
