package observability

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestQueryStoreListsFilteredPreviewsAndReturnsFullDetail(t *testing.T) {
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "audit-query.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	projectID := queryInt64(t, database, `SELECT id FROM projects WHERE slug = 'ungrouped'`)
	tokenID := insertQueryFixture(t, database, `
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('audit-agent', 'audit-hash', 'audit-prefix', datetime('now'), datetime('now'))`)
	targetID := insertQueryFixture(t, database, `
		INSERT INTO connector_targets (connector_kind, name, project_id, created_at, updated_at)
		VALUES ('ssh', 'audit-worker', ?, datetime('now'), datetime('now'))`, projectID)
	profileID := insertQueryFixture(t, database, `
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, created_at, updated_at)
		VALUES (?, 'ssh', 'private_key', 'main', datetime('now'), datetime('now'))`, targetID)
	runtimeID := insertQueryFixture(t, database, `
		INSERT INTO connector_runtime_surfaces (connector_kind, target_id, profile_id, capability_kind, created_at, updated_at)
		VALUES ('ssh', ?, ?, 'live_console', datetime('now'), datetime('now'))`, targetID, profileID)
	payload := `{"detail":"` + strings.Repeat("x", 700) + ` searchable audit phrase"}`
	auditID := insertQueryFixture(t, database, `
		INSERT INTO audit_logs (
			actor_type, token_id, project_id, runtime_id, connector_kind, target_id,
			profile_id, action_request_id, action, lifecycle_phase, payload_json, created_at
		) VALUES ('mcp', ?, ?, ?, 'ssh', ?, ?, 77, 'connector.completed', 'completed', ?, '2026-01-02T00:00:00Z')`,
		tokenID, projectID, runtimeID, targetID, profileID, payload)
	insertQueryFixture(t, database, `
		INSERT INTO audit_logs (actor_type, action, lifecycle_phase, payload_json, created_at)
		VALUES ('user', 'settings.updated', 'updated', '{}', '2026-01-01T00:00:00Z')`)

	store := NewQueryStore(database)
	result, err := store.List(context.Background(), QueryFilter{
		Actor:         "mcp",
		ProjectID:     projectID,
		RuntimeID:     runtimeID,
		ConnectorKind: "ssh",
		TargetID:      targetID,
		Query:         "searchable audit phrase",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("result = %#v", result)
	}
	item := result.Items[0]
	if item.ID != auditID || item.TokenName != "audit-agent" || item.ProjectName != "Ungrouped" || item.TargetName != "audit-worker" {
		t.Fatalf("joined metadata = %#v", item)
	}
	if item.TokenID == nil || *item.TokenID != tokenID || item.ProfileID == nil || *item.ProfileID != profileID || item.ActionRequestID == nil || *item.ActionRequestID != 77 {
		t.Fatalf("identity metadata = %#v", item)
	}
	if len(item.PayloadJSON) > 500 || strings.Contains(item.PayloadJSON, "searchable audit phrase") {
		t.Fatalf("list payload was not bounded to a preview: %q", item.PayloadJSON)
	}

	detail, err := store.Get(context.Background(), auditID)
	if err != nil {
		t.Fatalf("get audit log: %v", err)
	}
	if !strings.Contains(detail.PayloadJSON, "searchable audit phrase") {
		t.Fatalf("detail payload was truncated: %q", detail.PayloadJSON)
	}
	if _, err := store.Get(context.Background(), auditID+1000); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing audit error = %v, want sql.ErrNoRows", err)
	}
}

func TestQueryStoreFallsBackForPunctuationOnlySearch(t *testing.T) {
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "audit-punctuation.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	insertQueryFixture(t, database, `
		INSERT INTO audit_logs (actor_type, action, lifecycle_phase, payload_json, created_at)
		VALUES ('user', 'punctuation.check', 'observed', '{"query":"--"}', datetime('now'))`)

	result, err := NewQueryStore(database).List(context.Background(), QueryFilter{Query: "--", Limit: 10})
	if err != nil {
		t.Fatalf("list punctuation search: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("punctuation result = %#v", result)
	}
}

func TestQueryStoreRejectsInvalidBoundaries(t *testing.T) {
	if _, err := NewQueryStore(nil).List(context.Background(), QueryFilter{Limit: 1}); err == nil {
		t.Fatal("expected unavailable executor error")
	}
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "audit-boundary.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := NewQueryStore(database)
	if _, err := store.List(context.Background(), QueryFilter{}); err == nil {
		t.Fatal("expected non-positive limit error")
	}
	if _, err := store.List(context.Background(), QueryFilter{Limit: 1, Offset: -1}); err == nil {
		t.Fatal("expected negative offset error")
	}
	if _, err := store.Get(context.Background(), 0); err == nil {
		t.Fatal("expected non-positive id error")
	}
}

func insertQueryFixture(t *testing.T, database *sql.DB, statement string, args ...any) int64 {
	t.Helper()
	result, err := database.Exec(statement, args...)
	if err != nil {
		t.Fatalf("insert audit query fixture: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read audit query fixture id: %v", err)
	}
	return id
}

func queryInt64(t *testing.T, database *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var value int64
	if err := database.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query fixture integer: %v", err)
	}
	return value
}
