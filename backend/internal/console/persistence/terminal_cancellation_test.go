//go:build cgo

package persistence_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	sqlite3 "github.com/SE-I-T-Digital/go-sqlcipher"
	consolepersistence "github.com/aipermission/aipermission/backend/internal/console/persistence"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

func TestCanceledTranscriptRetainsWarmConnectionAndRollsBackChunks(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	if _, err := database.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO console_sessions (id, runtime_id, name, status, transcript, created_at, updated_at)
		VALUES (7, 1, 'test', 'connected', 'original', '2026-09-12T00:00:00Z', '2026-09-12T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var original *sqlite3.SQLiteConn
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Raw(func(raw any) error {
		original = raw.(*sqlite3.SQLiteConn)
		original.RegisterAuthorizer(func(operation int, table, _, _ string) int {
			if operation == sqlite3.SQLITE_UPDATE && table == "console_sessions" {
				cancel()
				// Keep this an acknowledged rollback; interrupt auto-rollback
				// correctly retires the connection and is a separate invariant.
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	})
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = consolepersistence.PersistTranscript(ctx, database, 7, "changed", "pending output", "2026-09-12T00:01:00Z")
	var nativeErr sqlite3.Error
	if !errors.Is(ctx.Err(), context.Canceled) || !errors.As(err, &nativeErr) || nativeErr.Code != sqlite3.ErrAuth || !transactionstate.IsNotCommitted(err) {
		t.Fatalf("canceled rejected transcript must acknowledge rollback: ctx=%v err=%v", ctx.Err(), err)
	}
	connection, err = database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Raw(func(raw any) error {
		next := raw.(*sqlite3.SQLiteConn)
		next.RegisterAuthorizer(nil)
		if next != original {
			t.Error("canceled transcript discarded the warm encrypted connection")
		}
		if !next.AutoCommit() {
			t.Error("canceled transcript left a transaction open")
		}
		return nil
	})
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	var snapshot string
	var chunks int
	if err := database.QueryRow("SELECT transcript FROM console_sessions WHERE id = 7").Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM console_session_chunks WHERE session_id = 7").Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if snapshot != "original" || chunks != 0 {
		t.Fatalf("canceled write escaped rollback: snapshot=%q chunks=%d", snapshot, chunks)
	}
	if err := consolepersistence.PersistTranscript(t.Context(), database, 7, "next", "next output", "2026-09-12T00:02:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT transcript FROM console_sessions WHERE id = 7").Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM console_session_chunks WHERE session_id = 7").Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if snapshot != "next" || chunks != 1 {
		t.Fatalf("subsequent write was not committed exactly once: snapshot=%q chunks=%d", snapshot, chunks)
	}
}
