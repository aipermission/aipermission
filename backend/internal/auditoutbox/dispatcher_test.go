package auditoutbox_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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
	var eventVersion int
	var lifecyclePhase string
	if err := database.QueryRow(`SELECT event_id, event_version, lifecycle_phase FROM audit_logs WHERE event_id = ?`, event.EventID).Scan(&projectedID, &eventVersion, &lifecyclePhase); err != nil {
		t.Fatal(err)
	}
	if eventVersion != auditoutbox.EventVersion || lifecyclePhase != "created" {
		t.Fatalf("projected metadata version=%d phase=%q", eventVersion, lifecyclePhase)
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
	if _, err := database.Exec(`UPDATE audit_outbox SET next_attempt_at = datetime('now', '-1 second') WHERE event_id = ?`, event.EventID); err != nil {
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
	health, err = (auditoutbox.Store{}).Health(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if health.PendingCount != 0 || health.FailureCount != 0 || health.LastDeliveryError != "" || health.LastDeliverySuccess == "" {
		t.Fatalf("dispatcher health did not recover: %#v", health)
	}
}

func TestDispatcherReportsRetryBookkeepingFailure(t *testing.T) {
	database := openAuditDatabase(t)
	appendAuditEvent(t, database, "project.created")
	if _, err := database.Exec(`
		CREATE TRIGGER reject_audit_projection BEFORE INSERT ON audit_logs
		BEGIN SELECT RAISE(ABORT, 'injected projection failure'); END;
		CREATE TRIGGER reject_audit_retry BEFORE UPDATE ON audit_outbox
		BEGIN SELECT RAISE(ABORT, 'injected retry bookkeeping failure'); END;`); err != nil {
		t.Fatal(err)
	}
	_, err := auditoutbox.NewDispatcher(database).DispatchOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "record audit delivery retry") {
		t.Fatalf("unexpected dispatcher error: %v", err)
	}
}

func TestDispatcherContinuesPastPoisonEventAndDeadLettersIt(t *testing.T) {
	database := openAuditDatabase(t)
	poison := appendAuditEvent(t, database, "poison.event")
	healthy := appendAuditEvent(t, database, "healthy.event")
	if _, err := database.Exec(`
		CREATE TRIGGER reject_poison_audit_projection BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'poison.event'
		BEGIN SELECT RAISE(ABORT, 'injected poison event'); END`); err != nil {
		t.Fatal(err)
	}
	dispatcher := auditoutbox.NewDispatcher(database)
	delivered, err := dispatcher.DispatchOnce(context.Background())
	if err == nil || delivered != 1 {
		t.Fatalf("first dispatch delivered=%d error=%v", delivered, err)
	}
	var healthyDelivered int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE event_id = ? AND delivered_at IS NOT NULL`, healthy.EventID).Scan(&healthyDelivered); err != nil {
		t.Fatal(err)
	}
	if healthyDelivered != 1 {
		t.Fatal("healthy event was blocked by poison event")
	}

	for attempt := 1; attempt < 8; attempt++ {
		if _, err := database.Exec(`UPDATE audit_outbox SET next_attempt_at = datetime('now', '-1 second') WHERE event_id = ?`, poison.EventID); err != nil {
			t.Fatal(err)
		}
		if _, err := dispatcher.DispatchOnce(context.Background()); err == nil {
			t.Fatalf("attempt %d unexpectedly succeeded", attempt+1)
		}
	}
	var attempts int
	var deadLettered sql.NullString
	if err := database.QueryRow(`SELECT attempt_count, dead_lettered_at FROM audit_outbox WHERE event_id = ?`, poison.EventID).Scan(&attempts, &deadLettered); err != nil {
		t.Fatal(err)
	}
	if attempts != 8 || !deadLettered.Valid {
		t.Fatalf("poison event attempts=%d dead_lettered=%v", attempts, deadLettered.Valid)
	}
	health, err := (auditoutbox.Store{}).Health(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if health.PendingCount != 0 || health.DeadLetterCount != 1 {
		t.Fatalf("unexpected dead-letter health: %#v", health)
	}
}

func TestDispatcherRecoversEventCommittedBeforeProcessRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.db")
	database, err := db.OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	event := appendAuditEvent(t, database, "project.created")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := db.OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if count, err := auditoutbox.NewDispatcher(reopened).DispatchOnce(context.Background()); err != nil || count != 1 {
		t.Fatalf("restart dispatch count=%d error=%v", count, err)
	}
	var projected int
	if err := reopened.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE event_id = ?`, event.EventID).Scan(&projected); err != nil {
		t.Fatal(err)
	}
	if projected != 1 {
		t.Fatalf("projected event count = %d", projected)
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
		ActorType: "user", Action: action, LifecyclePhase: "created", PayloadJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}
