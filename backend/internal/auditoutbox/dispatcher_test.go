package auditoutbox_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestDispatcherProjectsAndMarksEventDelivered(t *testing.T) {
	database := openAuditDatabase(t)
	event := appendAuditEvent(t, database, "project.created")
	dispatcher := auditoutbox.NewDispatcher(database)

	count, err := dispatcher.DispatchOnce(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("dispatch count=%d error=%v", count, err)
	}
	var projectedID string
	if err := database.QueryRow(`SELECT event_id FROM audit_logs WHERE event_id = ?`, event.EventID).Scan(&projectedID); err != nil {
		t.Fatal(err)
	}
	var deliveredAt string
	if err := database.QueryRow(`SELECT delivered_at FROM audit_outbox WHERE event_id = ?`, event.EventID).Scan(&deliveredAt); err != nil {
		t.Fatal(err)
	}
	if deliveredAt == "" {
		t.Fatal("event was not marked delivered")
	}
}

func TestDispatcherConvergesWhenProjectionAlreadyExists(t *testing.T) {
	database := openAuditDatabase(t)
	event := appendAuditEvent(t, database, "project.created")
	if _, err := database.Exec(`
		INSERT INTO audit_logs (event_id, actor_type, action, payload_json, created_at)
		VALUES (?, 'user', 'project.created', '{}', datetime('now'))`, event.EventID); err != nil {
		t.Fatal(err)
	}
	dispatcher := auditoutbox.NewDispatcher(database)
	if count, err := dispatcher.DispatchOnce(context.Background()); err != nil || count != 1 {
		t.Fatalf("dispatch count=%d error=%v", count, err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE event_id = ?`, event.EventID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("projection count = %d", count)
	}
}

func TestDispatcherPersistsFailureAndRecoversAfterRestart(t *testing.T) {
	database := openAuditDatabase(t)
	event := appendAuditEvent(t, database, "project.created")
	if _, err := database.Exec(`CREATE TRIGGER reject_audit_projection BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, 'injected projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := auditoutbox.NewDispatcher(database).DispatchOnce(context.Background()); err == nil {
		t.Fatal("expected injected projection failure")
	}
	health, err := (auditoutbox.Store{}).Health(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if health.PendingCount != 1 || health.RetriedEventCount != 1 || health.FailureCount != 1 || health.LastDeliveryError == "" {
		t.Fatalf("unexpected durable health: %#v", health)
	}
	if _, err := database.Exec(`DROP TRIGGER reject_audit_projection`); err != nil {
		t.Fatal(err)
	}
	restarted := auditoutbox.NewDispatcher(database)
	if count, err := restarted.DispatchOnce(context.Background()); err != nil || count != 1 {
		t.Fatalf("restart dispatch count=%d error=%v", count, err)
	}
	var delivered int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE event_id = ? AND delivered_at IS NOT NULL`, event.EventID).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if delivered != 1 {
		t.Fatal("restarted dispatcher did not recover pending event")
	}
}

func openAuditDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func appendAuditEvent(t *testing.T, database *sql.DB, action string) auditoutbox.Event {
	t.Helper()
	event, err := (auditoutbox.Store{}).Append(context.Background(), database, auditoutbox.Event{
		ActorType: "user", Action: action, PayloadJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}
