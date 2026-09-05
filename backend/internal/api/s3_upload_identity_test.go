package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestS3MultipartOriginalFilenameIdentity(t *testing.T) {
	for _, name := range []string{"a/../invoice", " invoice ", "/invoice", "a//invoice"} {
		t.Run(name, func(t *testing.T) {
			requests := make(chan string, 4)
			objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("unexpected method %s", r.Method)
				}
				requests <- r.URL.Path
			}))
			defer objectStore.Close()
			fixture := newAPITestFixture(t)
			id := createS3IdentityRuntime(t, fixture.server, objectStore.URL).TransferRuntimeID
			body, contentType := multipartUploadBody(t, map[string]string{"runtime_id": strconv.FormatInt(id, 10), "remote_dir": "/prefix//", "overwrite": "true"}, map[string][]byte{name: []byte("data")})
			req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/upload-batch", body)
			req.Header.Set("Content-Type", contentType)
			status := filetransfer.StatusPendingApproval
			handlers := fileTransferHandlers{fixture.server}
			runtime := fixture.server.activeRuntime()
			batch, _, _, ok := handlers.createUploadBatchFromMultipart(httptest.NewRecorder(), req, runtime, filetransfer.SourceMCP, &status, nil, nil)
			if !ok {
				t.Fatal("multipart preparation failed")
			}
			want := "/prefix//" + name
			if batch.Items[0].RemotePath != want {
				t.Fatalf("stored locator = %q, want %q", batch.Items[0].RemotePath, want)
			}
			_, _, err := runtime.fileTransfers.ApproveBatch(context.Background(), batch.ID, filetransfer.BatchApprovalRequest{ApprovedItemIDs: []int64{batch.Items[0].ID}})
			if err != nil {
				t.Fatal(err)
			}
			handlers.runTransferBatch(context.Background(), runtime, batch.ID, true)
			item, err := runtime.fileTransfers.Get(context.Background(), batch.Items[0].ID)
			if err != nil || item.Status != filetransfer.StatusCompleted {
				t.Fatalf("upload: %#v %v", item, err)
			}
			select {
			case got := <-requests:
				if got != "/identity-bucket"+want {
					t.Fatalf("upload target = %q", got)
				}
			case <-time.After(time.Second):
				t.Fatal("missing upload")
			}
		})
	}
}
