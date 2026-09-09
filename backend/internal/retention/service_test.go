package retention

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestUpdateCommitsSettingsCleanupAndAuditAtomically(t *testing.T) {
	database := openTestDatabase(t)
	insertHistory(t, database, "old", time.Now().AddDate(0, 0, -10))
	service := NewService(database, "workspace")

	deleted, err := service.Update(t.Context(), Settings{HistoryDays: 7}, auditRunner(database, nil))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["history"] != 1 {
		t.Fatalf("deleted = %#v", deleted)
	}
	settings, err := service.Read(t.Context())
	if err != nil || settings.HistoryDays != 7 {
		t.Fatalf("settings = %#v, err=%v", settings, err)
	}
	assertCount(t, database, "history_entries", 0)
	assertCountWhere(t, database, "audit_logs", "action = 'settings.retention.updated'", 1)

	insertHistory(t, database, "rollback", time.Now().AddDate(0, 0, -10))
	forced := errors.New("forced audit failure")
	_, err = service.Update(t.Context(), Settings{HistoryDays: 3}, auditRunner(database, forced))
	if !errors.Is(err, forced) {
		t.Fatalf("update error = %v", err)
	}
	settings, err = service.Read(t.Context())
	if err != nil || settings.HistoryDays != 7 {
		t.Fatalf("rolled-back settings = %#v, err=%v", settings, err)
	}
	assertCountWhere(t, database, "history_entries", "title = 'rollback'", 1)
	assertCountWhere(t, database, "audit_logs", "action = 'settings.retention.updated'", 1)
}

func TestAuditPurgeNeverDeletesPendingOutboxEvents(t *testing.T) {
	database := openTestDatabase(t)
	old := time.Now().AddDate(0, 0, -90).UTC().Format(time.RFC3339Nano)
	for _, row := range []struct {
		id, delivered, deadLettered string
	}{
		{id: "pending"},
		{id: "delivered", delivered: old},
		{id: "dead-lettered", deadLettered: old},
	} {
		if _, err := database.Exec(`
			INSERT INTO audit_outbox (
				event_id, event_version, actor_type, action, payload_json,
				occurred_at, created_at, delivered_at, dead_lettered_at
			) VALUES (?, 1, 'user', 'test', '{}', ?, ?, NULLIF(?, ''), NULLIF(?, ''))`,
			row.id, old, old, row.delivered, row.deadLettered,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO audit_logs (actor_type, action, payload_json, created_at) VALUES ('user', 'old', '{}', ?)`, old); err != nil {
		t.Fatal(err)
	}

	deleted, err := NewService(database, "workspace").purge(t.Context(), "audit", 30)
	if err != nil || deleted != 1 {
		t.Fatalf("purge = %d, %v", deleted, err)
	}
	assertCountWhere(t, database, "audit_outbox", "event_id = 'pending'", 1)
	assertCountWhere(t, database, "audit_outbox", "event_id IN ('delivered', 'dead-lettered')", 0)
}

func TestConfiguredCleanupPrunesExpiredIdempotencyWhenRetentionIsDisabled(t *testing.T) {
	database := openTestDatabase(t)
	insertExpiredIdempotency(t, database)
	service := NewService(database, "workspace")
	service.interval = 0
	service.Start()
	service.Stop()
	assertCount(t, database, "connector_action_idempotency_tombstones", 0)
	assertCount(t, database, "file_transfer_start_idempotency", 0)
}

func TestWorkerAppliesChangedSettingsAndStopsSynchronously(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database, "workspace")
	service.interval = 5 * time.Millisecond
	service.Start()
	t.Cleanup(service.Stop)
	done := service.done
	if done == nil {
		t.Fatal("worker was not started")
	}
	if err := writeSettings(t.Context(), database, service.repository, Settings{HistoryDays: 2, AuditDays: 2}); err != nil {
		t.Fatal(err)
	}
	insertHistory(t, database, "old", time.Now().AddDate(0, 0, -3))
	insertHistory(t, database, "recent", time.Now())
	old := time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339Nano)
	recent := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO audit_logs (actor_type, action, payload_json, created_at) VALUES ('test', 'old', '{}', ?), ('test', 'recent', '{}', ?)`, old, recent); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM history_entries`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		var auditCount int
		if err := database.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount); err != nil {
			t.Fatal(err)
		}
		if count == 1 && auditCount == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic cleanup did not run; history=%d audit=%d", count, auditCount)
		}
		time.Sleep(5 * time.Millisecond)
	}
	service.Stop()
	select {
	case <-done:
	default:
		t.Fatal("Stop returned before the worker exited")
	}
	if service.cancel != nil || service.done != nil {
		t.Fatal("worker lifecycle state was not cleared")
	}
}

func TestStartReappliesConfiguredRetentionWithoutDuplicatingWorker(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database, "workspace")
	service.Start()
	t.Cleanup(service.Stop)
	firstDone := service.done
	if err := writeSettings(t.Context(), database, service.repository, Settings{HistoryDays: 2}); err != nil {
		t.Fatal(err)
	}
	insertHistory(t, database, "old", time.Now().AddDate(0, 0, -3))

	service.Start()
	assertCount(t, database, "history_entries", 0)
	if service.done != firstDone {
		t.Fatal("reinitialization replaced the active worker")
	}
}

func TestConcurrentLifecycleKeepsOneWorkerGeneration(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database, "workspace")
	service.interval = time.Hour
	service.Start()

	firstDone := service.done
	results := make(chan chan struct{}, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			service.Start()
			service.lifecycleMu.Lock()
			results <- service.done
			service.lifecycleMu.Unlock()
		}()
	}
	group.Wait()
	close(results)
	for done := range results {
		if done != firstDone {
			t.Fatal("concurrent Start created another worker generation")
		}
	}

	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			service.Stop()
		}()
	}
	group.Wait()
	select {
	case <-firstDone:
	default:
		t.Fatal("concurrent Stop returned before the worker exited")
	}
	service.lifecycleMu.Lock()
	if service.cancel != nil || service.done != nil {
		service.lifecycleMu.Unlock()
		t.Fatal("concurrent Stop did not clear worker state")
	}
	service.lifecycleMu.Unlock()

	service.Start()
	service.lifecycleMu.Lock()
	cancel, done := service.cancel, service.done
	service.lifecycleMu.Unlock()
	if cancel == nil || done == nil || done == firstDone {
		t.Fatal("service did not restart one retention worker")
	}
	service.Stop()
}

func TestHTTPHandlersPreserveStrictRetentionContract(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database, "workspace")
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Service: service, Mutate: auditRunner(database, nil)}, true
	})

	updated := requestJSON(t, handlers.Update, http.MethodPut, "/api/settings/retention", Settings{HistoryDays: 7})
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"history_days":7`) {
		t.Fatalf("update response = %d %s", updated.Code, updated.Body.String())
	}
	read := httptest.NewRecorder()
	handlers.Get(read, httptest.NewRequest(http.MethodGet, "/api/settings/retention", nil))
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"history_days":7`) {
		t.Fatalf("read response = %d %s", read.Code, read.Body.String())
	}
	for name, response := range map[string]*httptest.ResponseRecorder{
		"negative settings": requestJSON(t, handlers.Update, http.MethodPut, "/api/settings/retention", Settings{HistoryDays: -1}),
		"zero days":         requestJSON(t, handlers.Purge, http.MethodPost, "/api/settings/retention/purge", PurgeRequest{Target: "history"}),
		"unknown target":    requestJSON(t, handlers.Purge, http.MethodPost, "/api/settings/retention/purge", PurgeRequest{Target: "unknown", Days: 7}),
	} {
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s response = %d %s", name, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPut, "/api/settings/retention", strings.NewReader(`{"history_days":7,"unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	strict := httptest.NewRecorder()
	handlers.Update(strict, request)
	if strict.Code != http.StatusBadRequest {
		t.Fatalf("unknown field response = %d %s", strict.Code, strict.Body.String())
	}

	missing := NewHTTPHandlers(nil)
	response := httptest.NewRecorder()
	missing.Get(response, httptest.NewRequest(http.MethodGet, "/api/settings/retention", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing scope response = %d", response.Code)
	}
	readOnly := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Service: service}, true
	})
	response = httptest.NewRecorder()
	readOnly.Get(response, httptest.NewRequest(http.MethodGet, "/api/settings/retention", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("read-only scope response = %d %s", response.Code, response.Body.String())
	}
}

func auditRunner(database *sql.DB, failAfterMutation error) auditedmutation.Runner {
	return func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := mutate(tx); err != nil {
			return err
		}
		if failAfterMutation != nil {
			return failAfterMutation
		}
		encoded, err := json.Marshal(payload())
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (actor_type, action, payload_json, created_at) VALUES ('user', ?, ?, ?)`, action, string(encoded), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.Commit()
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "retention.db"), "RetentionPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertHistory(t *testing.T, database *sql.DB, title string, timestamp time.Time) {
	t.Helper()
	value := timestamp.UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type,
			status, title, created_at, completed_at, updated_at
		) VALUES (
			'test',
			(SELECT COALESCE(MAX(source_ref_id), 0) + 1 FROM history_entries WHERE source_ref_type = 'test'),
			'test', 'test', 'completed', ?, ?, ?, ?
		)`, title, value, value, value); err != nil {
		t.Fatal(err)
	}
}

func insertExpiredIdempotency(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO connector_action_idempotency_tombstones (
			idempotency_scope, idempotency_key, idempotency_identity_hash, request_id,
			token_id, target_id, target_name, profile_id, profile_label, connector_kind,
			action_name, source, retry_policy_json, status, completed_at, retained_at, expires_at
		) VALUES (
			'source:manual', 'expired', 'identity', 999, NULL, 1, 'target', 1, 'profile', 'test',
			'read', 'manual', '{}', 'completed', '2000-01-01T00:00:00Z',
			'2000-01-01T00:00:00Z', '2000-01-02T00:00:00Z'
		)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO file_transfer_start_idempotency (
			scope, idempotency_key, identity_hash, resource_kind, resource_id, created_at, expires_at
		) VALUES ('ui', 'expired-transfer', 'h1:expired', 'batch', 1, '2000-01-01T00:00:00Z', '2000-01-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
}

func requestJSON(t *testing.T, handler http.HandlerFunc, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func assertCount(t *testing.T, database *sql.DB, table string, want int) {
	t.Helper()
	assertCountWhere(t, database, table, "1 = 1", want)
}

func assertCountWhere(t *testing.T, database *sql.DB, table, where string, want int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE ` + where).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}
