package db

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/timeformat"
)

func TestChronologicalTimestampMigrationNormalizesOrderingColumns(t *testing.T) {
	database, err := OpenEncrypted(filepath.Join(t.TempDir(), "timestamps.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type,
			status, title, created_at, updated_at
		) VALUES
			('test', 1, 'ssh', 'command', 'completed', 'older', '2026-01-01T00:00:00.1Z', '2026-01-01 00:00:00'),
			('test', 2, 'ssh', 'command', 'completed', 'newer', '2026-01-01T00:00:00.11Z', '2026-01-01T00:00:00.11Z');
		INSERT INTO audit_logs (actor_type, action, payload_json, created_at)
		VALUES ('test', 'older', '{}', '2026-01-01T00:00:00.1Z'),
		       ('test', 'newer', '{}', '2026-01-01T00:00:00.11Z');
	`); err != nil {
		t.Fatal(err)
	}
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := normalizeChronologicalTimestamps(tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var firstHistory, firstAudit string
	if err := database.QueryRow(`SELECT title FROM history_entries ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&firstHistory); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT action FROM audit_logs WHERE actor_type = 'test' ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&firstAudit); err != nil {
		t.Fatal(err)
	}
	if firstHistory != "newer" || firstAudit != "newer" {
		t.Fatalf("chronological order = history %q audit %q", firstHistory, firstAudit)
	}
	var older, newer, updated string
	if err := database.QueryRow(`
		SELECT
			MAX(CASE WHEN title = 'older' THEN created_at END),
			MAX(CASE WHEN title = 'newer' THEN created_at END),
			MAX(CASE WHEN title = 'older' THEN updated_at END)
		FROM history_entries WHERE source_ref_type = 'test'`).Scan(&older, &newer, &updated); err != nil {
		t.Fatal(err)
	}
	if older != "2026-01-01T00:00:00.100000000Z" || newer != "2026-01-01T00:00:00.110000000Z" || updated != "2026-01-01T00:00:00.000000000Z" {
		t.Fatalf("normalized values = older %q newer %q updated %q", older, newer, updated)
	}
}

func TestChronologicalTimestampMigrationRewritesActiveAuditWriters(t *testing.T) {
	database, err := OpenEncrypted(filepath.Join(t.TempDir(), "audit-writers.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.Query(`SELECT name, sql FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'audit_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name, statement string
		if err := rows.Scan(&name, &statement); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(statement, "strftime(") {
			continue
		}
		count++
		if strings.Contains(statement, "%fZ") || !strings.Contains(statement, "%f000000Z") {
			t.Fatalf("audit trigger %s is not a canonical timestamp writer: %s", name, statement)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected active audit triggers")
	}
}

func TestCanonicalAuditTriggerWritesSortWithGoTimestamps(t *testing.T) {
	database, err := OpenEncrypted(filepath.Join(t.TempDir(), "audit-order.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	targetID, profileID := insertConnectorTargetAndProfile(t, database)
	runtimeID := insertConnectorRuntimeSurface(t, database, "postgres", targetID, profileID, "structured_activity")
	directTimestamp := timeformat.UTC(time.Now().UTC().Add(-time.Second))
	if _, err := database.Exec(`
		INSERT INTO audit_outbox (
			event_id, event_version, actor_type, runtime_id, action, payload_json, occurred_at, created_at
		) VALUES ('direct-go-writer', 1, 'test', ?, 'direct.go', '{}', ?, ?)`, runtimeID, directTimestamp, directTimestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO command_requests (runtime_id, command, reason, status, created_at)
		VALUES (?, 'SELECT 1', 'timestamp test', 'completed', ?)`, runtimeID, timeformat.UTC(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	var triggerTimestamp string
	if err := database.QueryRow(`SELECT occurred_at FROM audit_outbox WHERE action = 'console.command.completed'`).Scan(&triggerTimestamp); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSuffix(triggerTimestamp, "Z"), ".")
	if len(parts) != 2 || len(parts[1]) != 9 {
		t.Fatalf("trigger timestamp is not canonical: %q", triggerTimestamp)
	}
	rows, err := database.Query(`SELECT action FROM audit_outbox WHERE action IN ('direct.go', 'console.command.completed') ORDER BY occurred_at DESC, id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actions := []string{}
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	if len(actions) != 2 || actions[0] != "console.command.completed" || actions[1] != "direct.go" {
		t.Fatalf("interleaved audit order = %#v", actions)
	}
}

func TestChronologicalTimestampMigrationRejectsMalformedMetadata(t *testing.T) {
	database, err := OpenEncrypted(filepath.Join(t.TempDir(), "malformed-timestamp.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		INSERT INTO audit_logs (actor_type, action, payload_json, created_at)
		VALUES ('test', 'malformed', '{}', 'not-a-timestamp')`); err != nil {
		t.Fatal(err)
	}
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := normalizeChronologicalTimestamps(tx); err == nil {
		tx.Rollback()
		t.Fatal("expected malformed timestamp to reject migration")
	}
	_ = tx.Rollback()
}
