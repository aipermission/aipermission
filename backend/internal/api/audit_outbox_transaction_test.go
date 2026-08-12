package api

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestAuditedMutationRollsBackDomainAndEventOnMutationFailure(t *testing.T) {
	database := openAuditTransactionDatabase(t)
	runtime := &databaseRuntime{database: database}
	server := &Server{}

	err := server.withAuditedMutation(
		context.Background(), runtime, "user", nil, 0, "settings.test.updated",
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
	assertAuditTransactionCounts(t, database, 0, 0)
}

func TestAuditedMutationRollsBackDomainWhenOutboxAppendFails(t *testing.T) {
	database := openAuditTransactionDatabase(t)
	if _, err := database.Exec(`
		CREATE TRIGGER reject_audit_outbox BEFORE INSERT ON audit_outbox
		BEGIN SELECT RAISE(ABORT, 'injected outbox failure'); END`); err != nil {
		t.Fatal(err)
	}
	runtime := &databaseRuntime{database: database}
	server := &Server{}

	err := server.withAuditedMutation(
		context.Background(), runtime, "user", nil, 0, "settings.test.updated",
		func() any { return map[string]any{"key": "audit-test"} },
		func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('audit-test', 'value', datetime('now'))`)
			return err
		},
	)
	if err == nil {
		t.Fatal("expected injected outbox failure")
	}
	assertAuditTransactionCounts(t, database, 0, 0)
}

func TestAuditedMutationCommitsWhenProjectionFails(t *testing.T) {
	database := openAuditTransactionDatabase(t)
	if _, err := database.Exec(`
		CREATE TRIGGER reject_audit_projection BEFORE INSERT ON audit_logs
		BEGIN SELECT RAISE(ABORT, 'injected projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	runtime := &databaseRuntime{database: database, auditDispatcher: auditoutbox.NewDispatcher(database)}
	server := &Server{}

	err := server.withAuditedMutation(
		context.Background(), runtime, "user", nil, 0, "settings.test.updated",
		func() any { return map[string]any{"key": "audit-test"} },
		func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('audit-test', 'value', datetime('now'))`)
			return err
		},
	)
	if err != nil {
		t.Fatalf("projection failure should not roll back committed mutation: %v", err)
	}
	assertAuditTransactionCounts(t, database, 1, 1)
	var delivered sql.NullString
	var attempts int
	if err := database.QueryRow(`SELECT delivered_at, attempt_count FROM audit_outbox`).Scan(&delivered, &attempts); err != nil {
		t.Fatal(err)
	}
	if delivered.Valid || attempts != 1 {
		t.Fatalf("failed projection state delivered=%v attempts=%d", delivered.Valid, attempts)
	}
}

func TestAuditRetentionNeverDeletesUndeliveredEvents(t *testing.T) {
	database := openAuditTransactionDatabase(t)
	if _, err := (auditoutbox.Store{}).Append(context.Background(), database, auditoutbox.Event{
		ActorType: "user", Action: "settings.test.updated", PayloadJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE audit_outbox SET created_at = datetime('now', '-90 days')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO audit_logs (actor_type, action, payload_json, created_at)
		VALUES ('user', 'old.observation', '{}', datetime('now', '-90 days'))`); err != nil {
		t.Fatal(err)
	}

	deleted, err := purgeRetentionTargetWithExecutor(context.Background(), database, "audit", 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted audit log count = %d, want 1", deleted)
	}
	var pending int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE delivered_at IS NULL`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("undelivered audit event was removed: pending=%d", pending)
	}
}

func TestAuditRetentionDeletesOnlyDeliveredOldOutboxEvents(t *testing.T) {
	database := openAuditTransactionDatabase(t)
	event, err := (auditoutbox.Store{}).Append(context.Background(), database, auditoutbox.Event{
		ActorType: "user", Action: "settings.test.updated", PayloadJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auditoutbox.NewDispatcher(database).DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE audit_outbox SET delivered_at = datetime('now', '-90 days') WHERE event_id = ?`, event.EventID); err != nil {
		t.Fatal(err)
	}
	if _, err := purgeRetentionTargetWithExecutor(context.Background(), database, "audit", 30); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE event_id = ?`, event.EventID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("delivered old outbox event was retained: count=%d", count)
	}
}

func openAuditTransactionDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "AuditPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func assertAuditTransactionCounts(t *testing.T, database *sql.DB, settingCount int, outboxCount int) {
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
