//go:build cgo

package filetransfer

import (
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestMCPUploadStatusAndExpiryUseKeyedActionIndex(t *testing.T) {
	database, _, _ := batchListFixture(t)
	tokenID := int64(1)
	for _, probe := range []struct {
		name, query string
		args        []any
	}{
		{"status", mcpUploadStatusSQL, []any{tokenID, connectortargets.ActionIdempotencyScope(&tokenID, SourceMCP), "ssh", int64(1), int64(1), "upload_file", mcpUploadScope(1), "missing-stage", IdempotencyResourceTransfer}},
		{"expiry", `SELECT f.id FROM file_transfers f WHERE f.source = ? AND f.direction = ? AND f.status = ? AND ` + mcpUploadDisposableStageSQL,
			[]any{SourceMCP, DirectionUpload, StatusPending, time.Now().UTC().Format(mcpUploadTimeLayout), connectortargets.TokenIdempotencyScopePrefix}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			rows, err := database.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+probe.query, probe.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var plan []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(plan, "; ")
			if !strings.Contains(joined, "idx_connector_action_requests_idempotency (idempotency_scope=? AND idempotency_key=?)") {
				t.Fatalf("upload action lookup scans retained history instead of the token/key index: %s", joined)
			}
		})
	}
}
