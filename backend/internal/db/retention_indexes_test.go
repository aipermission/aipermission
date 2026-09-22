package db

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestRetentionExpressionIndexesSupportCleanupQueries(t *testing.T) {
	database, err := OpenEncrypted(t.TempDir()+"/retention.db", "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	for _, test := range []struct {
		table string
		index string
	}{
		{table: "history_entries", index: "idx_history_entries_retention_completed"},
		{table: "vault_action_requests", index: "idx_vault_action_requests_retention_completed"},
	} {
		t.Run(test.table, func(t *testing.T) {
			assertRetentionExpressionIndex(t, database, test.table, test.index)
		})
	}
}

func assertRetentionExpressionIndex(t *testing.T, database *sql.DB, table, indexName string) {
	t.Helper()
	var indexSQL string
	if err := database.QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE type = 'index' AND name = ?`, indexName).Scan(&indexSQL); err != nil {
		t.Fatalf("read retention index: %v", err)
	}
	if !strings.Contains(indexSQL, "julianday(completed_at)") {
		t.Fatalf("retention index does not preserve mixed timestamp semantics: %s", indexSQL)
	}

	rows, err := database.Query(fmt.Sprintf(`
		EXPLAIN QUERY PLAN
		SELECT id FROM %s
		WHERE completed_at IS NOT NULL
			AND julianday(completed_at) < julianday('now', '-2 days')`, table))
	if err != nil {
		t.Fatalf("explain retention query: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id int
		var parent int
		var unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatalf("scan query plan: %v", err)
		}
		plan.WriteString(detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read query plan: %v", err)
	}
	if !strings.Contains(plan.String(), indexName) {
		t.Fatalf("retention query did not use %s: %s", indexName, plan.String())
	}
}
