package servicebaseline

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/SE-I-T-Digital/go-sqlcipher"
)

func TestStoreScopesAndReplacesBaselines(t *testing.T) {
	database := openBaselineStore(t)
	validID := func(value string) bool { return strings.HasPrefix(value, "backup-") }
	first := Baseline{BackupID: "backup-a", CreatedAt: "2026-09-20T10:00:00Z"}
	if err := Write(t.Context(), database, "https://backup.example", "stream-a", first, validID); err != nil {
		t.Fatal(err)
	}
	loaded, err := Read(t.Context(), database, "https://backup.example", "stream-a", validID)
	if err != nil || loaded == nil || *loaded != first {
		t.Fatalf("loaded baseline = %#v, err=%v", loaded, err)
	}
	missing, err := Read(t.Context(), database, "https://backup.example", "stream-b", validID)
	if err != nil || missing != nil {
		t.Fatalf("missing baseline = %#v, err=%v", missing, err)
	}
	second := Baseline{BackupID: "backup-b", CreatedAt: "2026-09-20T11:00:00.123Z"}
	if err := Write(t.Context(), database, "https://backup.example", "stream-a", second, validID); err != nil {
		t.Fatal(err)
	}
	loaded, err = Read(t.Context(), database, "https://backup.example", "stream-a", validID)
	if err != nil || loaded == nil || *loaded != second {
		t.Fatalf("replaced baseline = %#v, err=%v", loaded, err)
	}
}

func TestStoreRejectsInvalidBaselineData(t *testing.T) {
	database := openBaselineStore(t)
	validID := func(value string) bool { return strings.HasPrefix(value, "backup-") }
	for _, baseline := range []Baseline{{CreatedAt: "2026-09-20T10:00:00Z"}, {BackupID: "backup-a", CreatedAt: "invalid"}} {
		if err := Write(t.Context(), database, "https://backup.example", "stream-a", baseline, validID); err == nil {
			t.Fatalf("invalid baseline was accepted: %#v", baseline)
		}
	}
	if _, err := database.Exec(`INSERT INTO settings(key, value, updated_at) VALUES (?, ?, ?)`, key("https://backup.example", "stream-a"), `{`, "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(t.Context(), database, "https://backup.example", "stream-a", validID); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("malformed baseline error = %v", err)
	}
}

func openBaselineStore(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return database
}
