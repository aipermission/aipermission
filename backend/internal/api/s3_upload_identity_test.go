package api

import (
	"context"
	"encoding/json"
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
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte("<ListBucketResult/>"))
					return
				}
				if r.Method != http.MethodPut {
					t.Errorf("unexpected method %s", r.Method)
					return
				}
				requests <- r.URL.Path
			}))
			defer objectStore.Close()
			fixture := newAPITestFixture(t)
			id := createS3IdentityRuntime(t, fixture.server, objectStore.URL).TransferRuntimeID
			body, contentType := multipartUploadBody(t, map[string]string{
				"runtime_id": strconv.FormatInt(id, 10), "remote_dir": "/prefix//", "overwrite": "true",
				"idempotency_key": "identity-" + strconv.Itoa(len(name)) + "-" + strconv.Itoa(len([]byte(name))),
			}, map[string][]byte{name: []byte("data")})
			response := performMultipartRequest(fixture.server.Handler(), "/api/file-transfers/upload-batch", body, contentType)
			if response.Code != http.StatusAccepted {
				t.Fatalf("start multipart upload: %d %s", response.Code, response.Body.String())
			}
			var batch filetransfer.BatchRecord
			if err := json.Unmarshal(response.Body.Bytes(), &batch); err != nil {
				t.Fatalf("decode multipart upload: %v", err)
			}
			runtime := fixture.server.activeRuntime()
			want := "/prefix//" + name
			if batch.Items[0].RemotePath != want {
				t.Fatalf("stored locator = %q, want %q", batch.Items[0].RemotePath, want)
			}
			waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if !requireTransferJobs(t, fixture.server, runtime).Wait(waitCtx) {
				cancel()
				t.Fatal("multipart upload did not finish")
			}
			cancel()
			item, err := filetransfer.NewStore(fixture.db).Get(context.Background(), batch.Items[0].ID)
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
