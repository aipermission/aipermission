package persistence_test

import (
	"path/filepath"
	"strings"
	"testing"

	consolepersistence "github.com/aipermission/aipermission/backend/internal/console/persistence"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestPersistTranscriptChunksPendingOutputAtomically(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	if _, err := database.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO console_sessions (id, runtime_id, name, status, created_at, updated_at) VALUES (7, 1, 'test', 'connected', '2026-09-12T00:00:00Z', '2026-09-12T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	pending := strings.Repeat("x", consolepersistence.MaxChunkLength+11)
	if err := consolepersistence.PersistTranscript(t.Context(), database, 7, "tail", pending, "2026-09-12T00:01:00Z"); err != nil {
		t.Fatal(err)
	}
	var snapshot string
	if err := database.QueryRow("SELECT transcript FROM console_sessions WHERE id = 7").Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var count, bytes int
	if err := database.QueryRow("SELECT COUNT(*), COALESCE(SUM(length(data)), 0) FROM console_session_chunks WHERE session_id = 7").Scan(&count, &bytes); err != nil {
		t.Fatal(err)
	}
	if snapshot != "tail" || count != 2 || bytes != len(pending) {
		t.Fatalf("snapshot=%q chunks=%d bytes=%d", snapshot, count, bytes)
	}
}

func TestPersistTerminalStatusClosesSession(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	if _, err := database.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO console_sessions (id, runtime_id, name, status, created_at, updated_at) VALUES (9, 1, 'test', 'connected', '2026-09-12T00:00:00Z', '2026-09-12T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if err := consolepersistence.PersistTerminalStatus(t.Context(), database, 9, "error", "lost transport", "2026-09-12T00:01:00Z"); err != nil {
		t.Fatal(err)
	}
	var status, message, closedAt string
	if err := database.QueryRow("SELECT status, error, closed_at FROM console_sessions WHERE id = 9").Scan(&status, &message, &closedAt); err != nil {
		t.Fatal(err)
	}
	if status != "error" || message != "lost transport" || closedAt != "2026-09-12T00:01:00Z" {
		t.Fatalf("status=%q message=%q closed_at=%q", status, message, closedAt)
	}
}
