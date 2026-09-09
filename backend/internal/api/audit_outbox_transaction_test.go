package api

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

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
