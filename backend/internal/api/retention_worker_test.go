package api

import (
	"context"
	"testing"
	"time"
)

func TestPeriodicRetentionCleansUnlockedWorkspace(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	fixture.server.stopRetentionWorker(runtime)
	fixture.server.retentionInterval = 10 * time.Millisecond

	settings := retentionSettingsResponse{HistoryDays: 2, AuditDays: 2}
	if err := writeRetentionSettings(context.Background(), runtime, settings); err != nil {
		t.Fatalf("write retention settings: %v", err)
	}
	old := time.Now().UTC().AddDate(0, 0, -3).Format(time.RFC3339Nano)
	recent := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := fixture.db.Exec(`
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type,
			status, title, created_at, completed_at, updated_at
		) VALUES
			('test', 1, 'test', 'test', 'completed', 'old', ?, ?, ?),
			('test', 2, 'test', 'test', 'completed', 'recent', ?, ?, ?)`,
		old, old, old, recent, recent, recent,
	); err != nil {
		t.Fatalf("insert history fixtures: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO audit_logs (actor_type, action, payload_json, created_at)
		VALUES ('test', 'old', '{}', ?), ('test', 'recent', '{}', ?)`, old, recent); err != nil {
		t.Fatalf("insert audit fixtures: %v", err)
	}

	fixture.server.startRetentionWorker(runtime)
	deadline := time.Now().Add(time.Second)
	for {
		var historyCount int
		var auditCount int
		if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM history_entries`).Scan(&historyCount); err != nil {
			t.Fatalf("count history entries: %v", err)
		}
		if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount); err != nil {
			t.Fatalf("count audit logs: %v", err)
		}
		if historyCount == 1 && auditCount == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic retention did not run: history=%d audit=%d", historyCount, auditCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	var historyTitle string
	if err := fixture.db.QueryRow(`SELECT title FROM history_entries`).Scan(&historyTitle); err != nil {
		t.Fatalf("read retained history: %v", err)
	}
	if historyTitle != "recent" {
		t.Fatalf("retained history title=%q, want recent", historyTitle)
	}
}

func TestPeriodicRetentionPrunesExpiredIdempotencyTombstonesWhenHistoryRetentionIsDisabled(t *testing.T) {
	fixture := newAPITestFixture(t)
	if _, err := fixture.db.Exec(`
		INSERT INTO connector_action_idempotency_tombstones (
			idempotency_scope, idempotency_key, idempotency_identity_hash, request_id,
			token_id, target_id, target_name, profile_id, profile_label, connector_kind,
			action_name, source, retry_policy_json, status, completed_at, retained_at, expires_at
		) VALUES (
			'source:manual', 'expired', 'identity', 999, NULL, 1, 'target', 1, 'profile', 'test',
			'read', 'manual', '{}', 'completed', '2000-01-01T00:00:00Z',
			'2000-01-01T00:00:00Z', '2000-01-02T00:00:00Z'
		)`); err != nil {
		t.Fatalf("insert expired tombstone: %v", err)
	}

	fixture.server.runPeriodicRetention(t.Context(), fixture.server.activeRuntime())
	assertTableCount(t, fixture.db, "connector_action_idempotency_tombstones", 0)
}

func TestStopRetentionWorkerWaitsForShutdown(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	done := runtime.retentionDone
	if done == nil {
		t.Fatal("retention worker was not started")
	}

	fixture.server.stopRetentionWorker(runtime)
	select {
	case <-done:
	default:
		t.Fatal("retention worker did not stop")
	}
	if runtime.retentionCancel != nil || runtime.retentionDone != nil {
		t.Fatal("retention worker lifecycle state was not cleared")
	}
}
