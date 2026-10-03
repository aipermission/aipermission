package workspacelifecycle

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestSQLCipherRawKeyUsesHexIdentityNotPassphraseText(t *testing.T) {
	for _, hexLength := range []int{64, 96, 160} {
		t.Run(fmt.Sprint(hexLength), func(t *testing.T) {
			material := strings.Repeat("Aa01", hexLength/4)
			password := "x'" + material + "'"
			if err := ValidatePassword(password, password); err == nil {
				t.Fatal("new workspace accepted a raw-key password")
			}
			path := filepath.Join(t.TempDir(), "raw-key.aipdb")
			database, err := db.OpenEncryptedForMigration(path, password)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec("CREATE TABLE raw_key_fixture (value TEXT); INSERT INTO raw_key_fixture VALUES ('encrypted page identity')"); err != nil {
				_ = database.Close()
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			alternateText := "X'" + strings.ToUpper(material) + "'"
			if alternateText == password {
				t.Fatal("probe needs distinct passphrase text")
			}
			reopened, err := db.OpenEncryptedForMigration(path, alternateText)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var marker string
			if err := reopened.QueryRow("SELECT value FROM raw_key_fixture").Scan(&marker); err != nil || marker != "encrypted page identity" {
				t.Fatalf("actual encrypted-page read = %q %v", marker, err)
			}
			if err := db.RekeyContext(t.Context(), reopened, password); err == nil {
				t.Fatal("password change accepted a raw-key destination")
			}
			if err := reopened.QueryRow("SELECT value FROM raw_key_fixture").Scan(&marker); err != nil || marker != "encrypted page identity" {
				t.Fatalf("rejected rekey changed encrypted database: %q %v", marker, err)
			}
			const replacement = "RecoveryPassphrase123"
			if err := ValidatePassword(replacement, replacement); err != nil {
				t.Fatal(err)
			}
			if err := db.RekeyContext(t.Context(), reopened, replacement); err != nil {
				t.Fatal(err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			if err := db.ValidateEncrypted(path, replacement); err != nil {
				t.Fatalf("raw-key database could not migrate to a passphrase: %v", err)
			}
		})
	}
}
