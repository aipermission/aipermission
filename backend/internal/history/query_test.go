package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
)

func TestQueryStoreListPreservesSummaryBoundaryPaginationAndLabels(t *testing.T) {
	database := openTestDB(t)
	tokenID := insertToken(t, database)
	targetID, profileID := insertTargetProfile(t, database, "postgres", "orders-db", "username_password", "readonly")
	store := NewStore(database)
	queryStore := NewQueryStore(database)

	firstID := insertConnectorActionRequest(t, database, tokenID, targetID, profileID)
	if err := store.SyncConnectorActionRequest(context.Background(), firstID); err != nil {
		t.Fatalf("sync first action: %v", err)
	}
	secondID := insertConnectorActionRequest(t, database, tokenID, targetID, profileID)
	if err := store.SyncConnectorActionRequest(context.Background(), secondID); err != nil {
		t.Fatalf("sync second action: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE history_entries
		SET created_at = CASE source_ref_id WHEN ? THEN '2026-01-01T00:00:01Z' ELSE '2026-01-01T00:00:02Z' END,
			output_text = 'sensitive detail', output_json = '{"rows":[1]}'
		WHERE source_ref_type = ? AND source_ref_id IN (?, ?)`,
		firstID, SourceConnectorActionRequest, firstID, secondID,
	); err != nil {
		t.Fatalf("prepare history rows: %v", err)
	}
	label, _, err := NewLabelStore(database).CreateOrGet(context.Background(), " Production ", "#AABBCC")
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO history_entry_labels (history_entry_id, label_id, created_at)
		VALUES (?, ?, datetime('now'))`, secondID, label.ID); err != nil {
		t.Fatalf("attach label: %v", err)
	}

	items, hasMore, err := queryStore.List(context.Background(), QueryFilter{
		ConnectorKind: "postgres",
		Query:         "orders-db",
		Limit:         1,
	})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(items) != 1 || !hasMore || items[0].SourceRefID != secondID {
		t.Fatalf("first page = %#v, hasMore=%v", items, hasMore)
	}
	if items[0].OutputText != "" || items[0].OutputJSON != "{}" {
		t.Fatalf("summary leaked detail output: text=%q json=%q", items[0].OutputText, items[0].OutputJSON)
	}
	if len(items[0].Labels) != 1 || items[0].Labels[0].Name != "Production" || items[0].Labels[0].Color != "#aabbcc" {
		t.Fatalf("summary labels = %#v", items[0].Labels)
	}

	next, hasMore, err := queryStore.List(context.Background(), QueryFilter{
		ConnectorKind: "postgres",
		Limit:         1,
		BeforeTime:    items[0].CreatedAt,
		BeforeID:      items[0].ID,
	})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(next) != 1 || hasMore || next[0].SourceRefID != firstID {
		t.Fatalf("second page = %#v, hasMore=%v", next, hasMore)
	}

	total, err := queryStore.Count(context.Background(), QueryFilter{ConnectorKind: "postgres", Query: "codex"})
	if err != nil {
		t.Fatalf("count matching token: %v", err)
	}
	if total != 2 {
		t.Fatalf("matching total = %d, want 2", total)
	}
}

func TestQueryStoreGetIncludesDetailAndNormalizesRetryPolicy(t *testing.T) {
	database := openTestDB(t)
	tokenID := insertToken(t, database)
	targetID, profileID := insertTargetProfile(t, database, "postgres", "orders-db", "username_password", "readonly")
	actionID := insertConnectorActionRequest(t, database, tokenID, targetID, profileID)
	if _, err := database.Exec(`
		UPDATE connector_action_requests
		SET retry_policy_json = '{"max_attempts":0,"initial_delay_ms":0,"max_delay_ms":0}',
			output_json = '{"rows":[1]}', display_text = 'one row'
		WHERE id = ?`, actionID); err != nil {
		t.Fatalf("prepare action: %v", err)
	}
	if err := NewStore(database).SyncConnectorActionRequest(context.Background(), actionID); err != nil {
		t.Fatalf("sync action: %v", err)
	}

	item, err := NewQueryStore(database).Get(context.Background(), actionID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if item.OutputText != "one row" || item.OutputJSON != `{"rows":[1]}` {
		t.Fatalf("detail output = text %q json %q", item.OutputText, item.OutputJSON)
	}
	var policy map[string]any
	if err := json.Unmarshal([]byte(item.RetryPolicyJSON), &policy); err != nil {
		t.Fatalf("decode retry policy %q: %v", item.RetryPolicyJSON, err)
	}
	if policy["max_attempts"] == float64(0) {
		t.Fatalf("retry policy was not normalized: %#v", policy)
	}
}

func TestQueryStoreTargetsFormatsProfileAndRuntimeReferences(t *testing.T) {
	database := openTestDB(t)
	tokenID := insertToken(t, database)
	targetID, profileID := insertTargetProfile(t, database, "ssh", "edge-vps", "private_key", "main")
	runtimeID := insertRuntimeSurface(t, database, "ssh", targetID, profileID, "live_console")
	store := NewStore(database)

	commandID := insertCommandRequest(t, database, tokenID, runtimeID)
	if err := store.SyncCommandRequest(context.Background(), commandID); err != nil {
		t.Fatalf("sync command: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE history_entries SET target_id = NULL, profile_id = NULL
		WHERE source_ref_type = ? AND source_ref_id = ?`, SourceCommandRequest, commandID); err != nil {
		t.Fatalf("prepare runtime-only history: %v", err)
	}
	actionID := insertConnectorActionRequest(t, database, tokenID, targetID, profileID)
	if _, err := database.Exec(`UPDATE connector_action_requests SET connector_kind = 'ssh' WHERE id = ?`, actionID); err != nil {
		t.Fatalf("prepare action kind: %v", err)
	}
	if err := store.SyncConnectorActionRequest(context.Background(), actionID); err != nil {
		t.Fatalf("sync action: %v", err)
	}

	items, err := NewQueryStore(database).Targets(context.Background())
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	refs := map[string]bool{}
	for _, item := range items {
		refs[item.Ref] = true
	}
	profileRef := fmt.Sprintf("ssh:%d:%d", targetID, profileID)
	runtimeRef := "runtime:" + strconv.FormatInt(runtimeID, 10)
	if !refs[profileRef] || !refs[runtimeRef] {
		t.Fatalf("target refs = %#v", refs)
	}
}

func TestLabelStoreNormalizesAndReusesLabels(t *testing.T) {
	database := openTestDB(t)
	store := NewLabelStore(database)

	first, created, err := store.CreateOrGet(context.Background(), "  Needs   Review ", "#ABCDEF")
	if err != nil || !created {
		t.Fatalf("create label: created=%v err=%v", created, err)
	}
	second, created, err := store.CreateOrGet(context.Background(), "needs review", "not-a-color")
	if err != nil || created {
		t.Fatalf("reuse label: created=%v err=%v", created, err)
	}
	if second.ID != first.ID || first.Name != "Needs Review" || first.Color != "#abcdef" {
		t.Fatalf("labels = first %#v second %#v", first, second)
	}
	if _, _, err := store.CreateOrGet(context.Background(), "", ""); err == nil {
		t.Fatal("expected empty label name to fail")
	}
}

func TestLabelStoreOwnsEntryRelationships(t *testing.T) {
	database := openTestDB(t)
	store := NewLabelStore(database)
	label, _, err := store.CreateOrGet(t.Context(), "Production", "")
	if err != nil {
		t.Fatal(err)
	}
	entryID := insertHistoryEntryForLabelTest(t, database)
	exists, err := store.EntryExists(t.Context(), entryID)
	if err != nil || !exists {
		t.Fatalf("entry exists = %v, err = %v", exists, err)
	}
	if attached, err := store.Attach(t.Context(), entryID, label.ID); err != nil || !attached {
		t.Fatalf("first attach = %v, err = %v", attached, err)
	}
	if attached, err := store.Attach(t.Context(), entryID, label.ID); err != nil || attached {
		t.Fatalf("duplicate attach = %v, err = %v", attached, err)
	}
	if err := store.Detach(t.Context(), entryID, label.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Detach(t.Context(), entryID, label.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing detach error = %v", err)
	}
	if err := store.Delete(t.Context(), label.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), label.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing delete error = %v", err)
	}
	exists, err = store.EntryExists(t.Context(), entryID+1000)
	if err != nil || exists {
		t.Fatalf("missing entry exists = %v, err = %v", exists, err)
	}
}

func insertHistoryEntryForLabelTest(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type,
			status, title, created_at, updated_at
		) VALUES ('label_test', 1, 'test', 'action', 'completed', 'label test', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
