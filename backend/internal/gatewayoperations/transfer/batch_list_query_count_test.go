//go:build cgo && sqlite_trace

package gatewaytransfer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	sqlite3 "github.com/SE-I-T-Digital/go-sqlcipher"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func TestListFileTransferBatchesConstantQueryCount(t *testing.T) {
	fixture, _ := seedHTTPBatchList(t)
	var statements atomic.Int64
	setTrace := func(config *sqlite3.TraceConfig) {
		conn, err := fixture.database.Conn(context.WithoutCancel(t.Context()))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.Raw(func(raw any) error { return raw.(*sqlite3.SQLiteConn).SetTrace(config) }); err != nil {
			t.Fatal(err)
		}
	}
	setTrace(&sqlite3.TraceConfig{EventMask: sqlite3.TraceStmt, Callback: func(sqlite3.TraceInfo) int {
		statements.Add(1)
		return 0
	}})
	t.Cleanup(func() { setTrace(nil) })
	for _, test := range []struct {
		query         string
		size, queries int
	}{
		{"limit=1", 1, 3}, {"limit=3&offset=1", 3, 3}, {"limit=100", 6, 3},
		{"limit=3&offset=6", 0, 2}, {"q=absent", 0, 2},
	} {
		t.Run(test.query, func(t *testing.T) {
			statements.Store(0)
			response := httptest.NewRecorder()
			fixture.handlers.ListFileTransferBatches(response, httptest.NewRequest(http.MethodGet, "/api/file-transfer-batches?"+test.query, nil))
			var page httptransport.PageResponse[filetransfer.BatchRecord]
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || len(page.Items) != test.size || statements.Load() != int64(test.queries) {
				t.Fatalf("status=%d size=%d SQL statements=%d; want size=%d SQL statements=%d", response.Code, len(page.Items), statements.Load(), test.size, test.queries)
			}
			t.Logf("HTTP page size=%d: %d SQL statements", len(page.Items), statements.Load())
		})
	}
}
