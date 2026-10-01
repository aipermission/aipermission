//go:build cgo && sqlite_trace

package filetransfer

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	sqlite3 "github.com/SE-I-T-Digital/go-sqlcipher"
)

func TestListBatchesConstantQueryCount(t *testing.T) {
	database, store, runtimeID := batchListFixture(t)
	for i := 0; i < 101; i++ {
		createListBatch(t, store, runtimeID, "backup", DirectionDownload, StatusPending)
	}
	var statements atomic.Int64
	setTrace := func(config *sqlite3.TraceConfig) {
		conn, err := database.Conn(context.WithoutCancel(t.Context()))
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
	for _, page := range []struct{ limit, offset, size, queries int }{
		{1, 0, 1, 3}, {3, 2, 3, 3}, {100, 1, 100, 3}, {3, 101, 0, 2},
	} {
		t.Run(fmt.Sprintf("limit%d-offset%d", page.limit, page.offset), func(t *testing.T) {
			statements.Store(0)
			items, total, err := store.ListBatches(t.Context(), BatchListFilter{Limit: page.limit, Offset: page.offset, Query: "backup"})
			if err != nil || total != 101 || len(items) != page.size {
				t.Fatalf("items=%d total=%d err=%v", len(items), total, err)
			}
			if got := statements.Load(); got != int64(page.queries) {
				t.Fatalf("executed SQL statements=%d, want %d", got, page.queries)
			}
			t.Logf("page size=%d: %d SQL statements", len(items), statements.Load())
		})
	}
}
