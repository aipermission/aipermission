package api

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func createS3IdentityRuntime(t *testing.T, server *Server, endpoint string) targetProfileItem {
	t.Helper()
	u, _ := url.Parse(endpoint)
	host, portText, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(portText)
	response := performJSON(server.Handler(), http.MethodPost, "/api/connector-targets/with-profile", "", createConnectorTargetWithProfileRequest{
		Target: createConnectorTargetRequest{ConnectorKind: "s3", Name: "identity-fixture", Config: map[string]any{
			"connection_mode": "direct", "scheme": "http", "host": host, "port": port,
			"region": "us-east-1", "bucket": "identity-bucket", "path_style": true, "trust_conditional_requests": true,
		}},
		Profile: createConnectorCredentialProfileRequest{Kind: "access_key", Label: "default", Public: map[string]any{"access_key_id": "fixture-access"}, Secret: map[string]any{"secret_access_key": "fixture-secret"}},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	response = performJSON(server.Handler(), http.MethodGet, "/api/targets", "", nil)
	var targets struct {
		Items []targetProfileItem `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &targets); err != nil || len(targets.Items) != 1 {
		t.Fatalf("targets: %s", response.Body.String())
	}
	return targets.Items[0]
}

func TestS3SingleWhitespaceKeyDownloadUsesFallbackLocalName(t *testing.T) {
	requests := make(chan string, 2)
	objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		if r.URL.Path != "/identity-bucket/ " {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", "4")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte("data"))
		}
	}))
	defer objectStore.Close()

	fixture := newAPITestFixture(t)
	runtimeID := createS3IdentityRuntime(t, fixture.server, objectStore.URL).TransferRuntimeID
	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download", "", startDownloadRequest{
		RuntimeID: runtimeID, RemotePath: "/ ", IdempotencyKey: "single-whitespace-key",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("start single download: %d %s", response.Code, response.Body.String())
	}
	var started filetransfer.Record
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode transfer: %v", err)
	}
	if started.RemotePath != "/ " || started.FileName != "aipermission-file" {
		t.Fatalf("stored identity = remote %q local %q", started.RemotePath, started.FileName)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !fixture.server.activeRuntime().transferJobs.Wait(waitCtx) {
		t.Fatal("single download did not finish")
	}
	completed, err := fixture.server.activeRuntime().fileTransfers.Get(context.Background(), started.ID)
	if err != nil || completed.Status != filetransfer.StatusCompleted {
		t.Fatalf("completed transfer = %#v, %v", completed, err)
	}
	if data, err := os.ReadFile(completed.TempPath); err != nil || string(data) != "data" {
		t.Fatalf("downloaded bytes = %q, %v", data, err)
	}
	download := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers/"+strconv.FormatInt(started.ID, 10)+"/download", "", nil)
	_, parameters, err := mime.ParseMediaType(download.Header().Get("Content-Disposition"))
	if download.Code != http.StatusOK || err != nil || parameters["filename"] != "aipermission-file" || download.Body.String() != "data" {
		t.Fatalf("served download: status=%d disposition=%q body=%q err=%v", download.Code, download.Header().Get("Content-Disposition"), download.Body.String(), err)
	}
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		select {
		case got := <-requests:
			if got != method+" /identity-bucket/ " {
				t.Fatalf("remote request = %q", got)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s request", method)
		}
	}
}

func TestS3TransferAPIExactIdentity(t *testing.T) {
	keys := []string{"invoice", "invoice ", " invoice", " ", "/invoice", "a//b", "a/../b", "a/./b", "caf\u00e9", "cafe\u0301", "a%2Fb", "a\\b"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			requests := make(chan string, 8)
			objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/identity-bucket" {
					if got := r.URL.Query().Get("prefix"); got != "a//" {
						t.Errorf("prefix = %q", got)
					}
					_, _ = fmt.Fprint(w, "<ListBucketResult><Contents><Key>")
					_ = xml.EscapeText(w, []byte(key))
					_, _ = fmt.Fprint(w, "</Key><Size>4</Size></Contents></ListBucketResult>")
					return
				}
				requests <- r.Method + " " + r.URL.Path
				if r.URL.Path != "/identity-bucket/"+key {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Length", "4")
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte("data"))
				}
			}))
			defer objectStore.Close()
			fixture := newAPITestFixture(t)
			id := createS3IdentityRuntime(t, fixture.server, objectStore.URL).TransferRuntimeID
			browse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/browse", "", browseRemoteFilesRequest{RuntimeID: id, Path: "/a//"})
			var page browseRemoteFilesResponse
			if err := json.Unmarshal(browse.Body.Bytes(), &page); err != nil || browse.Code != http.StatusOK || len(page.Entries) != 1 || page.Entries[0].Path != "/"+key || page.Parent != "/a/" {
				t.Fatalf("browse: %d %s", browse.Code, browse.Body.String())
			}
			// Approval persistence and execution must retain the selected locator.
			handlers := fileTransferHandlers{fixture.server}
			runtime := fixture.server.activeRuntime()
			batch, _, err := handlers.createDownloadBatch(context.Background(), runtime, id, []string{page.Entries[0].Path}, "", filetransfer.SourceMCP, filetransfer.StatusPendingApproval, "")
			if err != nil {
				t.Fatal(err)
			}
			if batch.Items[0].RemotePath != "/"+key {
				t.Fatalf("stored key = %q", batch.Items[0].RemotePath)
			}
			if key == " " && batch.Items[0].FileName != "aipermission-file" {
				t.Fatalf("whitespace key local filename = %q", batch.Items[0].FileName)
			}
			_, _, err = runtime.fileTransfers.ApproveBatch(context.Background(), batch.ID, filetransfer.BatchApprovalRequest{ApprovedItemIDs: []int64{batch.Items[0].ID}})
			if err != nil {
				t.Fatal(err)
			}
			handlers.runTransferBatch(context.Background(), runtime, batch.ID, false)
			item, err := runtime.fileTransfers.Get(context.Background(), batch.Items[0].ID)
			if err != nil || item.Status != filetransfer.StatusCompleted {
				t.Fatalf("transfer = %#v, %v", item, err)
			}
			data, err := os.ReadFile(item.TempPath)
			if err != nil || string(data) != "data" {
				t.Fatalf("bytes = %q, %v", data, err)
			}
			for _, method := range []string{"HEAD", "GET"} {
				select {
				case got := <-requests:
					if got != method+" /identity-bucket/"+key {
						t.Fatalf("request = %q", got)
					}
				case <-time.After(time.Second):
					t.Fatalf("missing %s", method)
				}
			}
		})
	}
}

func TestS3TransferRejectsUnsupportedBeforeDispatch(t *testing.T) {
	objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected dispatch: %s", r.URL) }))
	defer objectStore.Close()
	fixture := newAPITestFixture(t)
	id := createS3IdentityRuntime(t, fixture.server, objectStore.URL).TransferRuntimeID
	for index, paths := range [][]string{{"/folder/"}, {"/control\tkey"}, {"/a/../b", "/b"}, {"/a//b", "/a/b"}, {"//a", "/a"}, {"/.env", "/env"}, {"/" + strings.Repeat("x", 161), "/other"}} {
		response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download-batch", "", startDownloadBatchRequest{
			RuntimeID: id, RemotePaths: paths, IdempotencyKey: fmt.Sprintf("unsupported-path-%d", index),
		})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("paths %q: %d %s", paths, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "idempotency_key") {
			t.Fatalf("paths %q bypassed path validation: %s", paths, response.Body.String())
		}
	}
}

func TestS3CompletedDownloadBatchReplayPreservesArtifact(t *testing.T) {
	objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/identity-bucket/report.txt" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", "4")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte("data"))
		}
	}))
	defer objectStore.Close()

	fixture := newAPITestFixture(t)
	runtimeID := createS3IdentityRuntime(t, fixture.server, objectStore.URL).TransferRuntimeID
	request := startDownloadBatchRequest{
		RuntimeID:      runtimeID,
		RemotePaths:    []string{"/report.txt"},
		IdempotencyKey: "completed-download-replay",
	}
	first := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download-batch", "", request)
	if first.Code != http.StatusAccepted {
		t.Fatalf("start download batch: %d %s", first.Code, first.Body.String())
	}
	var started filetransfer.BatchRecord
	if err := json.Unmarshal(first.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode download batch: %v", err)
	}
	runtime := fixture.server.activeRuntime()
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !runtime.transferJobs.Wait(waitCtx) {
		t.Fatal("download batch did not finish")
	}
	completed, err := runtime.fileTransfers.GetBatch(context.Background(), started.ID)
	if err != nil || completed.Status != filetransfer.StatusCompleted || len(completed.Items) != 1 {
		t.Fatalf("completed batch = %#v, %v", completed, err)
	}
	artifactPath := completed.Items[0].TempPath
	if data, err := os.ReadFile(artifactPath); err != nil || string(data) != "data" {
		t.Fatalf("artifact before replay = %q, %v", data, err)
	}

	replay := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download-batch", "", request)
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay download batch: %d %s", replay.Code, replay.Body.String())
	}
	waitCtx, cancelReplay := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReplay()
	if !runtime.transferJobs.Wait(waitCtx) {
		t.Fatal("replayed download batch did not settle")
	}
	if data, err := os.ReadFile(artifactPath); err != nil || string(data) != "data" {
		t.Fatalf("artifact after replay = %q, %v", data, err)
	}
	fileTransferHandlers{fixture.server}.runTransferBatch(context.Background(), runtime, completed.ID, false)
	if data, err := os.ReadFile(artifactPath); err != nil || string(data) != "data" {
		t.Fatalf("artifact after terminal runner replay = %q, %v", data, err)
	}
}
