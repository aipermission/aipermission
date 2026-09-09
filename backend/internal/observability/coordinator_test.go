package observability

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestCoordinatorRollsBackDomainAndEventOnMutationFailure(t *testing.T) {
	database := openCoordinatorDatabase(t)
	coordinator := NewCoordinator(database, nil, nil, nil)

	err := coordinator.WithMutation(
		t.Context(), "user", nil, 0, "settings.test.updated",
		func() any { return map[string]any{"key": "audit-test"} },
		func(tx *sql.Tx) error {
			if _, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('audit-test', 'value', datetime('now'))`); err != nil {
				return err
			}
			return errors.New("injected domain mutation failure")
		},
	)
	if err == nil {
		t.Fatal("expected injected mutation failure")
	}
	assertCoordinatorCounts(t, database, 0, 0)
}

func TestCoordinatorRollsBackDomainWhenOutboxAppendFails(t *testing.T) {
	database := openCoordinatorDatabase(t)
	if _, err := database.Exec(`
		CREATE TRIGGER reject_audit_outbox BEFORE INSERT ON audit_outbox
		BEGIN SELECT RAISE(ABORT, 'injected outbox failure'); END`); err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(database, nil, nil, nil)

	err := coordinator.WithMutation(
		t.Context(), "user", nil, 0, "settings.test.updated",
		func() any { return map[string]any{"key": "audit-test"} },
		func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('audit-test', 'value', datetime('now'))`)
			return err
		},
	)
	if err == nil {
		t.Fatal("expected injected outbox failure")
	}
	assertCoordinatorCounts(t, database, 0, 0)
}

func TestCoordinatorCommitsWhenProjectionFails(t *testing.T) {
	database := openCoordinatorDatabase(t)
	if _, err := database.Exec(`
		CREATE TRIGGER reject_audit_projection BEFORE INSERT ON audit_logs
		BEGIN SELECT RAISE(ABORT, 'injected projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	var failures int
	var failedAction string
	coordinator := NewCoordinator(database, NewDispatcher(database), nil, func(action string, err error) {
		failures++
		failedAction = action
		if err == nil {
			t.Error("projection callback received nil error")
		}
	})

	err := coordinator.WithMutation(
		t.Context(), "user", nil, 0, "settings.test.updated",
		func() any { return map[string]any{"key": "audit-test"} },
		func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('audit-test', 'value', datetime('now'))`)
			return err
		},
	)
	if err != nil {
		t.Fatalf("projection failure should not roll back committed mutation: %v", err)
	}
	assertCoordinatorCounts(t, database, 1, 1)
	if failures != 1 || failedAction != "" {
		t.Fatalf("projection failure callback = (%d, %q), want (1, empty)", failures, failedAction)
	}
	var delivered sql.NullString
	var attempts int
	if err := database.QueryRow(`SELECT delivered_at, attempt_count FROM audit_outbox`).Scan(&delivered, &attempts); err != nil {
		t.Fatal(err)
	}
	if delivered.Valid || attempts != 1 {
		t.Fatalf("failed projection state delivered=%v attempts=%d", delivered.Valid, attempts)
	}
}

func TestCoordinatorWriteRequiredReportsProjectionAction(t *testing.T) {
	database := openCoordinatorDatabase(t)
	if _, err := database.Exec(`
		CREATE TRIGGER reject_audit_projection BEFORE INSERT ON audit_logs
		BEGIN SELECT RAISE(ABORT, 'injected projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	var failedAction string
	coordinator := NewCoordinator(database, NewDispatcher(database), nil, func(action string, _ error) {
		failedAction = action
	})
	if err := coordinator.WriteRequired(t.Context(), "user", nil, 0, "settings.test.updated", map[string]any{"ok": true}); err != nil {
		t.Fatalf("projection failure must remain best effort: %v", err)
	}
	if failedAction != "settings.test.updated" {
		t.Fatalf("projection callback action = %q", failedAction)
	}
}

func TestCoordinatorRejectsUnavailableDependenciesAndCallbacks(t *testing.T) {
	ctx := context.Background()
	var nilCoordinator *Coordinator
	if err := nilCoordinator.WriteRequired(ctx, "user", nil, 0, "test", map[string]any{}); err == nil {
		t.Fatal("expected nil coordinator write failure")
	}
	if err := NewCoordinator(nil, nil, nil, nil).WithTransaction(ctx, func(*sql.Tx, Appender) error { return nil }); err == nil {
		t.Fatal("expected unavailable database failure")
	}
	database := openCoordinatorDatabase(t)
	coordinator := NewCoordinator(database, nil, nil, nil)
	if err := coordinator.WithTransaction(ctx, nil); err == nil {
		t.Fatal("expected nil transaction callback failure")
	}
	if err := coordinator.WithMutation(ctx, "user", nil, 0, "test", nil, func(*sql.Tx) error { return nil }); err == nil {
		t.Fatal("expected nil payload callback failure")
	}
	if err := coordinator.WithMutation(ctx, "user", nil, 0, "test", func() any { return nil }, nil); err == nil {
		t.Fatal("expected nil mutation callback failure")
	}
}

func TestCoordinatorAppliesCapturedRedactor(t *testing.T) {
	database := openCoordinatorDatabase(t)
	coordinator := NewCoordinator(database, nil, func(value string) string {
		return strings.ReplaceAll(value, "secret", "[REDACTED]")
	}, nil)
	if err := coordinator.WriteRequired(t.Context(), "user", nil, 0, "test", map[string]any{"value": "secret"}); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := database.QueryRow(`SELECT payload_json FROM audit_outbox`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "secret") || !strings.Contains(payload, "[REDACTED]") {
		t.Fatalf("audit payload was not redacted: %s", payload)
	}
}

func openCoordinatorDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "AuditPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func assertCoordinatorCounts(t *testing.T, database *sql.DB, settingCount int, outboxCount int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'audit-test'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != settingCount {
		t.Fatalf("domain mutation count = %d, want %d", count, settingCount)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != outboxCount {
		t.Fatalf("outbox count = %d, want %d", count, outboxCount)
	}
}
