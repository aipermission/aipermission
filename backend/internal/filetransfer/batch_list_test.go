//go:build cgo

package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlite3 "github.com/SE-I-T-Digital/go-sqlcipher"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func batchListFixture(t *testing.T) (*sql.DB, *Store, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "batches.aipdb")
	database, err := dbpkg.OpenEncrypted(path, "batch-list-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if dbpkg.LooksLikePlainSQLite(path) {
		t.Fatal("fixture must be encrypted")
	}
	return database, NewStore(database), insertTestServer(t, database)
}

func createListBatch(t *testing.T, store *Store, runtimeID int64, name, direction, status string) BatchRecord {
	t.Helper()
	batch, err := store.CreateBatch(t.Context(), CreateBatchRequest{
		RuntimeID: runtimeID, Direction: direction, Source: SourceUI, Status: status,
		ArchiveName: name, Overwrite: true, ApprovalNote: "list fixture",
		Items: []CreateRequest{
			{FileName: "first.txt", RemotePath: "/first.txt", SizeBytes: 10, TempPath: "/private/first"},
			{FileName: "second.txt", RemotePath: "/second.txt", SizeBytes: 20},
			{FileName: "third.txt", RemotePath: "/third.txt", SizeBytes: 30},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

// Fault injection uses the real native connection, without replacing storage.
func observeBatchListSQL(t *testing.T, database *sql.DB, callback func(int, string, string, string) int) {
	t.Helper()
	set := func(callback func(int, string, string, string) int) {
		conn, err := database.Conn(context.WithoutCancel(t.Context()))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.Raw(func(raw any) error {
			raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(callback)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	set(callback)
	t.Cleanup(func() { set(nil) })
}

func TestListBatchesPageItems(t *testing.T) {
	database, store, runtimeID := batchListFixture(t)
	var batches []BatchRecord
	for i := 0; i < 105; i++ {
		batches = append(batches, createListBatch(t, store, runtimeID, fmt.Sprintf("archive-%03d", i), DirectionDownload, StatusPending))
	}
	if _, err := database.Exec(`UPDATE file_transfer_batches SET created_at = '2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	// Queue position wins over insertion order; equal positions use ascending id.
	if _, err := database.Exec(`UPDATE file_transfers SET queue_index = CASE WHEN queue_index = 0 THEN 2 ELSE 1 END`); err != nil {
		t.Fatal(err)
	}
	for _, page := range []struct{ limit, offset int }{{1, 0}, {3, 2}, {100, 1}, {3, 105}} {
		t.Run(fmt.Sprintf("limit%d-offset%d", page.limit, page.offset), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			items, total, err := store.ListBatches(ctx, BatchListFilter{Limit: page.limit, Offset: page.offset})
			if err != nil || total != 105 || items == nil {
				t.Fatalf("list: total=%d items=%v err=%v", total, items, err)
			}
			wantLen := min(page.limit, 105-page.offset)
			if len(items) != wantLen {
				t.Fatalf("items=%d; want %d", len(items), wantLen)
			}
			for i, item := range items {
				want, err := store.GetBatch(t.Context(), batches[104-page.offset-i].ID)
				if err != nil || !reflect.DeepEqual(item, want) {
					t.Fatalf("list/get mismatch: list=%+v get=%+v err=%v", item, want, err)
				}
				if item.Items[0].QueueIndex != 1 || item.Items[1].QueueIndex != 1 || item.Items[0].ID >= item.Items[1].ID || item.Items[2].QueueIndex != 2 {
					t.Fatalf("unexpected child order: %+v", item.Items)
				}
			}
		})
	}
}

func TestListBatchesLoadsOnlySelectedChildren(t *testing.T) {
	database, store, runtimeID := batchListFixture(t)
	offPage := createListBatch(t, store, runtimeID, "off-page", DirectionDownload, StatusPending)
	selected := createListBatch(t, store, runtimeID, "selected", DirectionDownload, StatusPending)
	if _, err := database.Exec(`UPDATE file_transfer_batches SET created_at = '2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE file_transfers SET failure_details_json = 'invalid-json' WHERE batch_id = ?`, offPage.ID); err != nil {
		t.Fatal(err)
	}
	items, total, err := store.ListBatches(t.Context(), BatchListFilter{Limit: 1})
	if err != nil || total != 2 || len(items) != 1 || items[0].ID != selected.ID || len(items[0].Items) != 3 {
		t.Fatalf("selected page: items=%+v total=%d err=%v", items, total, err)
	}
	if _, _, err := store.ListBatches(t.Context(), BatchListFilter{Limit: 1, Offset: 1}); err == nil {
		t.Fatal("selected malformed child must fail")
	}
	if _, err := database.Exec(`DELETE FROM file_transfers WHERE batch_id = ?`, selected.ID); err != nil {
		t.Fatal(err)
	}
	items, _, err = store.ListBatches(t.Context(), BatchListFilter{Limit: 1})
	if err != nil || len(items) != 1 || items[0].Items != nil {
		t.Fatalf("childless batch: items=%+v err=%v", items, err)
	}
}

func TestListBatchesFilters(t *testing.T) {
	database, store, runtimeID := batchListFixture(t)
	if _, err := database.Exec(`UPDATE connector_targets SET name = 'worker-primary'`); err != nil {
		t.Fatal(err)
	}
	// Make runtime ids diverge from target ids so authorization cannot confuse them.
	if _, err := database.Exec(`INSERT INTO connector_runtime_surfaces
		(connector_kind, target_id, profile_id, capability_kind, label, created_at, updated_at)
		SELECT connector_kind, target_id, profile_id, 'file_transfer', 'extra', created_at, updated_at
		FROM connector_runtime_surfaces WHERE id = ?`, runtimeID); err != nil {
		t.Fatal(err)
	}
	otherRuntimeID := insertTestServer(t, database)
	want := createListBatch(t, store, runtimeID, "wanted-backup", DirectionDownload, StatusPending)
	createListBatch(t, store, runtimeID, "upload-backup", DirectionUpload, StatusPending)
	createListBatch(t, store, runtimeID, "waiting-backup", DirectionDownload, StatusPendingApproval)
	other := createListBatch(t, store, otherRuntimeID, "other-backup", DirectionDownload, StatusPending)
	var targetID int64
	if err := database.QueryRow(`SELECT target_id FROM connector_runtime_surfaces WHERE id = ?`, runtimeID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []BatchListFilter{
		{Query: "wanted"},
		{Direction: " download ", Status: " pending ", RuntimeID: runtimeID, Query: " backup "},
		{Direction: DirectionDownload, Status: StatusPending, TargetIDs: []int64{targetID, targetID, -1}, Query: "worker-primary"},
	} {
		items, total, err := store.ListBatches(t.Context(), filter)
		if err != nil || total != 1 || len(items) != 1 || !reflect.DeepEqual(items[0], want) {
			t.Fatalf("filter %+v: items=%+v total=%d err=%v", filter, items, total, err)
		}
	}
	items, total, err := store.ListBatches(t.Context(), BatchListFilter{TargetIDs: []int64{targetID + 1000}})
	if err != nil || items == nil || len(items) != 0 || total != 0 {
		t.Fatalf("unauthorized target filter: items=%v total=%d err=%v", items, total, err)
	}
	var otherTargetID int64
	if err := database.QueryRow(`SELECT target_id FROM connector_runtime_surfaces WHERE id = ?`, otherRuntimeID).Scan(&otherTargetID); err != nil {
		t.Fatal(err)
	}
	items, total, err = store.ListBatches(t.Context(), BatchListFilter{TargetIDs: []int64{otherTargetID}})
	if err != nil || total != 1 || len(items) != 1 || !reflect.DeepEqual(items[0], other) {
		t.Fatalf("target ids must not be runtime ids: items=%+v total=%d err=%v", items, total, err)
	}
}

func TestListBatchesCreationOrder(t *testing.T) {
	database, store, runtimeID := batchListFixture(t)
	var ids []int64
	for i := 0; i < 3; i++ {
		ids = append(ids, createListBatch(t, store, runtimeID, "backup", DirectionDownload, StatusPending).ID)
	}
	if _, err := database.Exec(`UPDATE file_transfer_batches SET created_at = CASE WHEN id = ?
		THEN '2026-02-01T00:00:00Z' ELSE '2026-01-01T00:00:00Z' END`, ids[0]); err != nil {
		t.Fatal(err)
	}
	items, total, err := store.ListBatches(t.Context(), BatchListFilter{})
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("items=%+v total=%d err=%v", items, total, err)
	}
	for i, id := range []int64{ids[0], ids[2], ids[1]} {
		if items[i].ID != id || len(items[i].Items) != 3 {
			t.Fatalf("unexpected batch/child order: %+v", items)
		}
	}
}

func TestListBatchesErrorsAndCancellation(t *testing.T) {
	for _, stage := range []string{"count", "page", "children", "cancel-children"} {
		t.Run(stage, func(t *testing.T) {
			database, store, runtimeID := batchListFixture(t)
			createListBatch(t, store, runtimeID, "fixture", DirectionDownload, StatusPending)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var selects int
			observeBatchListSQL(t, database, func(op int, table, _, _ string) int {
				if op == sqlite3.SQLITE_SELECT {
					selects++
				}
				if (stage == "count" && selects == 1) || (stage == "page" && selects == 2) || (stage == "children" && table == "file_transfers") {
					return sqlite3.SQLITE_DENY
				}
				if stage == "cancel-children" && table == "file_transfers" {
					cancel()
				}
				return sqlite3.SQLITE_OK
			})
			items, total, err := store.ListBatches(ctx, BatchListFilter{})
			if err == nil || items != nil || total != 0 {
				t.Fatalf("must fail without a partial page: items=%v total=%d err=%v", items, total, err)
			}
			if stage == "cancel-children" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation not preserved: %v", err)
			}
			if stage == "children" && !strings.Contains(err.Error(), "batch items") {
				t.Fatalf("missing child error context: %v", err)
			}
		})
	}
	_, store, _ := batchListFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := store.ListBatches(ctx, BatchListFilter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled request: %v", err)
	}
}
