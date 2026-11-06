package legacymigration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationRejectsRawKeysBeforeOpeningOrCreatingFiles(t *testing.T) {
	for _, length := range []int{64, 96, 160} {
		for _, prefix := range []string{"x'", "X'"} {
			password := prefix + strings.Repeat("Aa01", length/4) + "'"
			t.Run(prefix+strings.Repeat("x", length/16), func(t *testing.T) {
				if err := validateNewDatabasePassword(password); err == nil || strings.Contains(err.Error(), password) {
					t.Fatalf("raw key accepted or disclosed: %v", err)
				}
				root := t.TempDir()
				_, err := MigrateLegacy010To020(t.Context(), Legacy010To020Request{DataPath: filepath.Join(root, "catalog"), SourceDatabaseID: "old", SourcePassword: "oldpass", TargetName: "New", TargetPassword: password})
				if err == nil || !strings.Contains(err.Error(), "passphrase") || strings.Contains(err.Error(), password) {
					t.Fatalf("migration did not reject raw key before source access: %v", err)
				}
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("rejected migration touched storage: %v (%d entries)", err, len(entries))
				}
			})
		}
	}
}

func TestMigrationPassphraseKeepsComplexityAndLiteralSemantics(t *testing.T) {
	for _, password := range []string{"OrdinaryPassword123", " x'" + strings.Repeat("Aa01", 16) + "' ", "Apostrophe'Password123"} {
		if err := validateNewDatabasePassword(password); err != nil {
			t.Fatalf("valid literal passphrase rejected: %v", err)
		}
	}
	for _, password := range []string{"Short1", "alllowercase1234", "ALLUPPERCASE1234", "NoNumbersInThisPassword"} {
		if err := validateNewDatabasePassword(password); err == nil {
			t.Fatal("existing strength rule bypassed")
		}
	}
}
