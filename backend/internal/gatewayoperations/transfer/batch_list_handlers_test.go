//go:build cgo

package gatewaytransfer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	sqlite3 "github.com/SE-I-T-Digital/go-sqlcipher"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func observeHTTPBatchListSQL(t *testing.T, fixture transferTestFixture, callback func(int, string, string, string) int) {
	t.Helper()
	set := func(callback func(int, string, string, string) int) {
		conn, err := fixture.database.Conn(context.WithoutCancel(t.Context()))
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

func seedHTTPBatchList(t *testing.T) (transferTestFixture, []filetransfer.BatchRecord) {
	t.Helper()
	fixture := newTransferTestFixture(t)
	fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) {
		return fixture.runtime, true
	}
	var batches []filetransfer.BatchRecord
	for i := 0; i < 6; i++ {
		direction := filetransfer.DirectionDownload
		if i == 0 {
			direction = filetransfer.DirectionUpload
		}
		batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
			RuntimeID: fixture.runtimeID, Direction: direction, Source: filetransfer.SourceUI,
			ArchiveName: fmt.Sprintf("backup-%d", i), Overwrite: true,
			Items: []filetransfer.CreateRequest{
				{RemotePath: "/first.txt", FileName: "first.txt", TempPath: "/private/staged"},
				{RemotePath: "/second.txt", FileName: "second.txt"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		batches = append(batches, batch)
	}
	if _, err := fixture.database.Exec(`UPDATE file_transfer_batches SET created_at = '2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	return fixture, batches
}

func TestListFileTransferBatchesNativePage(t *testing.T) {
	fixture, batches := seedHTTPBatchList(t)
	for _, test := range []struct {
		query         string
		limit, offset int
		total         int
		indexes       []int
		next          *int
	}{
		{query: "limit=1", limit: 1, total: 6, indexes: []int{5}, next: intPointer(1)},
		{query: "limit=3&offset=1", limit: 3, offset: 1, total: 6, indexes: []int{4, 3, 2}, next: intPointer(4)},
		{query: "limit=1000", limit: 100, total: 6, indexes: []int{5, 4, 3, 2, 1, 0}},
		{query: "limit=3&offset=6", limit: 3, offset: 6, total: 6},
		{query: "q=absent", limit: 50},
		{query: "q=backup-3&direction=download&status=pending", limit: 50, total: 1, indexes: []int{3}},
		{query: fmt.Sprintf("runtime_id=%d&direction=upload", fixture.runtimeID), limit: 50, total: 1, indexes: []int{0}},
		{query: "q=transfer%20fixture&direction=download", limit: 50, total: 5, indexes: []int{5, 4, 3, 2, 1}},
		{query: fmt.Sprintf("runtime_id=%d", fixture.runtimeID+1000), limit: 50},
	} {
		t.Run(test.query, func(t *testing.T) {
			response := httptest.NewRecorder()
			fixture.handlers.ListFileTransferBatches(response, httptest.NewRequest(http.MethodGet, "/api/file-transfer-batches?"+test.query, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var page httptransport.PageResponse[filetransfer.BatchRecord]
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.Items == nil || len(page.Items) != len(test.indexes) || page.Total != test.total || page.Limit != test.limit || page.Offset != test.offset || !reflect.DeepEqual(page.NextOffset, test.next) {
				t.Fatalf("unexpected page: %+v", page)
			}
			for i, index := range test.indexes {
				stored, err := fixture.store.GetBatch(t.Context(), batches[index].ID)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(stored)
				if err != nil {
					t.Fatal(err)
				}
				var want filetransfer.BatchRecord
				if err := json.Unmarshal(encoded, &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(page.Items[i], want) {
					t.Fatalf("list/get response mismatch: got=%+v want=%+v", page.Items[i], want)
				}
			}
			for _, private := range []string{"temp_path", "archive_path", "remote_staging_ref", "/private/staged"} {
				if strings.Contains(response.Body.String(), private) {
					t.Fatalf("private field/value exposed: %s", private)
				}
			}
		})
	}
}

func intPointer(value int) *int { return &value }

func TestListFileTransferBatchesNativeValidationAndFailures(t *testing.T) {
	fixture, _ := seedHTTPBatchList(t)
	var selects atomic.Int64
	var denyChildren atomic.Bool
	observeHTTPBatchListSQL(t, fixture, func(op int, table, _, _ string) int {
		if op == sqlite3.SQLITE_SELECT {
			selects.Add(1)
		}
		if denyChildren.Load() && table == "file_transfers" {
			return sqlite3.SQLITE_DENY
		}
		return sqlite3.SQLITE_OK
	})
	for _, query := range []string{"limit=0", "offset=-1", "direction=invalid", "status=invalid", "runtime_id=0", "runtime_id=invalid"} {
		selects.Store(0)
		response := httptest.NewRecorder()
		fixture.handlers.ListFileTransferBatches(response, httptest.NewRequest(http.MethodGet, "/api/file-transfer-batches?"+query, nil))
		if response.Code != http.StatusBadRequest || selects.Load() != 0 {
			t.Fatalf("query=%s status=%d SELECTs=%d", query, response.Code, selects.Load())
		}
	}
	for _, canceled := range []bool{false, true} {
		denyChildren.Store(!canceled)
		request := httptest.NewRequest(http.MethodGet, "/api/file-transfer-batches", nil)
		if canceled {
			ctx, cancel := context.WithCancel(request.Context())
			cancel()
			request = request.WithContext(ctx)
		}
		response := httptest.NewRecorder()
		fixture.handlers.ListFileTransferBatches(response, request)
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "backup-") || strings.Contains(response.Body.String(), "not authorized") {
			t.Fatalf("canceled=%v status=%d body=%s", canceled, response.Code, response.Body.String())
		}
	}
}
