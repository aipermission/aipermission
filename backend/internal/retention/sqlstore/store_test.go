package sqlstore

import (
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestStoreReadsWritesAndPurgesRetentionData(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "retention.db"), "RetentionPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := Store{}
	if err := store.WriteSetting(t.Context(), database, "retention_history_days", "7", "2026-09-13T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	settings, err := store.ReadSettings(t.Context(), database, []string{"retention_history_days", "missing"})
	if err != nil || settings["retention_history_days"] != "7" {
		t.Fatalf("settings = %#v, err=%v", settings, err)
	}
	for name, purge := range map[string]func() (int64, error){
		"history":     func() (int64, error) { return store.PurgeHistory(t.Context(), database, "-7 days") },
		"audit":       func() (int64, error) { return store.PurgeAudit(t.Context(), database, "-7 days") },
		"console":     func() (int64, error) { return store.PurgeConsole(t.Context(), database, "-7 days") },
		"messages":    func() (int64, error) { return store.PurgeMessages(t.Context(), database, "-7 days") },
		"idempotency": func() (int64, error) { return store.PurgeExpiredIdempotency(t.Context(), database) },
	} {
		if _, err := purge(); err != nil {
			t.Fatalf("purge %s: %v", name, err)
		}
	}
}
