package db

import (
	"strings"
	"testing"
)

func TestRetentionExpressionIndexesSupportCleanupQueries(t *testing.T) {
	database, err := OpenEncrypted(t.TempDir()+"/retention.db", "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	const indexName = "idx_history_entries_retention_completed"
	var indexSQL string
	if err := database.QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE type = 'index' AND name = ?`, indexName).Scan(&indexSQL); err != nil {
		t.Fatalf("read retention index: %v", err)
	}
	if !strings.Contains(indexSQL, "julianday(completed_at)") {
		t.Fatalf("retention index does not preserve mixed timestamp semantics: %s", indexSQL)
	}

	rows, err := database.Query(`
		EXPLAIN QUERY PLAN
		SELECT id FROM history_entries
		WHERE completed_at IS NOT NULL
			AND julianday(completed_at) < julianday('now', '-2 days')`)
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
