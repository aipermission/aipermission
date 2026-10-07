package retention

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

func automaticAuditRunner(database *sql.DB) auditedmutation.Runner {
	coordinator := observability.NewCoordinator(database, nil, nil, nil)
	return func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
		return coordinator.WithMutation(ctx, "gateway", nil, 0, action, payload, mutate)
	}
}

func TestAutomaticRetentionAuditsOnlyActualCleanup(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database, "workspace", automaticAuditRunner(database))
	if err := writeSettings(t.Context(), database, service.repository, Settings{HistoryDays: 2}); err != nil {
		t.Fatal(err)
	}
	insertHistory(t, database, "private-retention-title-canary", time.Now().AddDate(0, 0, -3))
	deleted, err := service.applyConfigured(t.Context())
	if err != nil || deleted["history"] != 1 {
		t.Fatalf("cleanup: %v %v", deleted, err)
	}
	assertCount(t, database, "history_entries", 0)
	assertCount(t, database, "audit_outbox", 1)
	var actor, payload string
	if err := database.QueryRow(`SELECT actor_type, payload_json FROM audit_outbox WHERE action = 'settings.retention.automatic_cleanup'`).Scan(&actor, &payload); err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Deleted     map[string]int64 `json:"deleted"`
		HistoryDays int              `json:"history_days"`
	}
	if err := json.Unmarshal([]byte(payload), &summary); err != nil {
		t.Fatal(err)
	}
	if actor != "gateway" || summary.Deleted["history"] != 1 || summary.HistoryDays != 2 || strings.Contains(payload, "canary") {
		t.Fatalf("summary: %s %s", actor, payload)
	}
	for range 3 {
		if _, err := service.applyConfigured(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	assertCount(t, database, "audit_outbox", 1)
}

func TestAutomaticRetentionAuditFailureRollsBackCleanup(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database, "workspace", automaticAuditRunner(database))
	if err := writeSettings(t.Context(), database, service.repository, Settings{HistoryDays: 2}); err != nil {
		t.Fatal(err)
	}
	insertHistory(t, database, "rollback-fixture", time.Now().AddDate(0, 0, -3))
	if _, err := database.Exec(`CREATE TRIGGER fail_automatic_summary BEFORE INSERT ON audit_outbox WHEN NEW.action = 'settings.retention.automatic_cleanup' BEGIN SELECT RAISE(ABORT, 'synthetic audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if deleted, err := service.applyConfigured(t.Context()); err == nil || deleted != nil {
		t.Fatalf("unaudited cleanup committed: %v %v", deleted, err)
	}
	assertCount(t, database, "history_entries", 1)
	assertCount(t, database, "audit_outbox", 0)
	if _, err := database.Exec(`DROP TRIGGER fail_automatic_summary`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.applyConfigured(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCount(t, database, "history_entries", 0)
	assertCount(t, database, "audit_outbox", 1)
}

func TestAutomaticRetentionRequiresAuditRunner(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database, "workspace", nil)
	insertExpiredIdempotency(t, database)
	if _, err := service.applyConfigured(t.Context()); !errors.Is(err, ErrMutationRunnerRequired) {
		t.Fatalf("missing audit runner: %v", err)
	}
	assertCount(t, database, "connector_action_idempotency_tombstones", 1)
}

func TestAutomaticRetentionDoesNotHideUncertainNoOpRollback(t *testing.T) {
	database := openTestDatabase(t)
	rollbackFailure := errors.New("synthetic rollback failure")
	runner := func(ctx context.Context, _ string, _ func() any, mutate func(*sql.Tx) error) error {
		err := transactionstate.Run(ctx, database, mutate)
		if !errors.Is(err, errNoRetainedRecords) || !transactionstate.IsNotCommitted(err) {
			t.Fatalf("expected no-op callback: %v", err)
		}
		return transactionstate.UnknownWithSafeReadback(errors.Join(err, rollbackFailure))
	}
	service := NewService(database, "workspace", runner)
	deleted, err := service.applyConfigured(t.Context())
	if deleted != nil || !errors.Is(err, rollbackFailure) || transactionstate.IsNotCommitted(err) {
		t.Fatalf("uncertain rollback was hidden: %v %v", deleted, err)
	}
	assertCount(t, database, "audit_outbox", 0)
}

func TestAutomaticRetentionAuditsOutboxOnlyDeletion(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database, "workspace", automaticAuditRunner(database))
	if err := writeSettings(t.Context(), database, service.repository, Settings{AuditDays: 2}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO audit_outbox (event_id, event_version, actor_type, action, payload_json, occurred_at, created_at, delivered_at) VALUES ('expired-delivered',1,'gateway','fixture','{}',?,?,?)`, old, old, old); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.applyConfigured(t.Context())
	if err != nil || deleted["audit"] != 1 {
		t.Fatalf("outbox cleanup: %v %v", deleted, err)
	}
	assertCountWhere(t, database, "audit_outbox", "action = 'settings.retention.automatic_cleanup'", 1)
	assertCountWhere(t, database, "audit_outbox", "event_id = 'expired-delivered'", 0)
}
