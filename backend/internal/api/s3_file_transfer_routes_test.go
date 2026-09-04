package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestS3ProfileExposesGenericFileTransferRuntime(t *testing.T) {
	uploadedPath := make(chan string, 1)
	objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Fatal("S3 request was not signed")
		}
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("prefix") == "reflect/" || r.URL.Query().Get("prefix") == "reflect-recursive/" {
				_, _ = w.Write([]byte(`<ListBucketResult><NextContinuationToken>test-secret</NextContinuationToken><Contents><Key>test-secret</Key><Size>12</Size></Contents></ListBucketResult>`))
				return
			}
			if r.URL.Query().Get("prefix") == "large/" {
				_, _ = w.Write([]byte(`<ListBucketResult><Contents><Key>large/object.bin</Key><Size>536870913</Size></Contents></ListBucketResult>`))
				return
			}
			if r.URL.Query().Get("prefix") == "daily/nested/report.txt/" {
				_, _ = w.Write([]byte(`<ListBucketResult/>`))
				return
			}
			if r.URL.Query().Get("continuation-token") == "page-2" {
				_, _ = w.Write([]byte(`<ListBucketResult><Contents><Key>daily/report-2.txt</Key><Size>14</Size></Contents></ListBucketResult>`))
				return
			}
			_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>page-2</NextContinuationToken><Contents><Key>daily/report.txt</Key><Size>12</Size></Contents></ListBucketResult>`))
		case http.MethodHead:
			if r.URL.Path == "/test-bucket/batch/a.bin" || r.URL.Path == "/test-bucket/batch/b.bin" || r.URL.Path == "/test-bucket/batch/c.bin" {
				w.Header().Set("Content-Length", "536870912")
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.URL.Path == "/test-bucket/large/object.bin" {
				w.Header().Set("Content-Length", "536870913")
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			_, _ = io.Copy(io.Discard, r.Body)
			uploadedPath <- r.URL.Path
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected S3 request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer objectStore.Close()
	endpoint, err := url.Parse(objectStore.URL)
	if err != nil {
		t.Fatalf("parse object store URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(endpoint.Host)
	if err != nil {
		t.Fatalf("split object store address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse object store port: %v", err)
	}

	fixture := newAPITestFixture(t)
	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-targets/with-profile", "", createConnectorTargetWithProfileRequest{
		Target: createConnectorTargetRequest{
			ConnectorKind: "s3",
			Name:          "test-objects",
			Config: map[string]any{
				"connection_mode":            "direct",
				"scheme":                     endpoint.Scheme,
				"host":                       host,
				"port":                       port,
				"region":                     "us-east-1",
				"bucket":                     "test-bucket",
				"path_style":                 true,
				"trust_conditional_requests": true,
			},
		},
		Profile: createConnectorCredentialProfileRequest{
			Kind:   "access_key",
			Label:  "default",
			Public: map[string]any{"access_key_id": "test-access"},
			Secret: map[string]any{"secret_access_key": "test-secret"},
		},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create S3 target: %d %s", response.Code, response.Body.String())
	}

	list := performJSON(fixture.server.Handler(), http.MethodGet, "/api/targets", "", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list targets: %d %s", list.Code, list.Body.String())
	}
	var payload struct {
		Items []targetProfileItem `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode target list: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].RuntimeID != 0 || payload.Items[0].TransferRuntimeID < 1 {
		t.Fatalf("unexpected S3 runtime surfaces: %#v", payload.Items)
	}

	browse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/browse", "", browseRemoteFilesRequest{
		RuntimeID: payload.Items[0].TransferRuntimeID,
		Path:      "/daily",
	})
	if browse.Code != http.StatusOK || !bytes.Contains(browse.Body.Bytes(), []byte(`"path":"/daily/report.txt"`)) || !bytes.Contains(browse.Body.Bytes(), []byte(`"has_more":true`)) {
		t.Fatalf("browse S3 transfer runtime: %d %s", browse.Code, browse.Body.String())
	}
	nextBrowse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/browse", "", browseRemoteFilesRequest{
		RuntimeID: payload.Items[0].TransferRuntimeID,
		Path:      "/daily",
		Cursor:    "page-2",
	})
	if nextBrowse.Code != http.StatusOK || !bytes.Contains(nextBrowse.Body.Bytes(), []byte(`"path":"/daily/report-2.txt"`)) || !bytes.Contains(nextBrowse.Body.Bytes(), []byte(`"has_more":false`)) {
		t.Fatalf("browse S3 transfer runtime next page: %d %s", nextBrowse.Code, nextBrowse.Body.String())
	}
	reflectedBrowse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/browse", "", browseRemoteFilesRequest{
		RuntimeID: payload.Items[0].TransferRuntimeID,
		Path:      "/reflect",
	})
	if reflectedBrowse.Code != http.StatusBadGateway || bytes.Contains(reflectedBrowse.Body.Bytes(), []byte("test-secret")) {
		t.Fatalf("browse exposed reflected S3 credential: %d %s", reflectedBrowse.Code, reflectedBrowse.Body.String())
	}
	reflectedExpand := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/expand", "", expandRemoteFilesRequest{
		RuntimeID: payload.Items[0].TransferRuntimeID,
		Path:      "/reflect-recursive",
	})
	if reflectedExpand.Code != http.StatusBadGateway || bytes.Contains(reflectedExpand.Body.Bytes(), []byte("test-secret")) {
		t.Fatalf("recursive selection exposed reflected S3 credential: %d %s", reflectedExpand.Code, reflectedExpand.Body.String())
	}
	oversizedExpand := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/expand", "", expandRemoteFilesRequest{
		RuntimeID: payload.Items[0].TransferRuntimeID,
		Path:      "/large",
	})
	if oversizedExpand.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized recursive selection status=%d body=%s", oversizedExpand.Code, oversizedExpand.Body.String())
	}
	oversizedDownload := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download", "", startDownloadRequest{
		RuntimeID:  payload.Items[0].TransferRuntimeID,
		RemotePath: "/large/object.bin",
	})
	if oversizedDownload.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized download status=%d body=%s", oversizedDownload.Code, oversizedDownload.Body.String())
	}
	runtime := fixture.server.activeRuntime()
	handlers := fileTransferHandlers{fixture.server}
	pendingBatch, err := handlers.createDownloadBatch(context.Background(), runtime, payload.Items[0].TransferRuntimeID, []string{
		"/batch/a.bin", "/batch/b.bin", "/batch/c.bin",
	}, "", filetransfer.SourceMCP, filetransfer.StatusPendingApproval)
	if err != nil {
		t.Fatalf("create pending approval download batch: %v", err)
	}
	approvedIDs := make([]int64, 0, len(pendingBatch.Items))
	for _, item := range pendingBatch.Items {
		approvedIDs = append(approvedIDs, item.ID)
	}
	approvedBatch, _, err := runtime.fileTransfers.ApproveBatch(context.Background(), pendingBatch.ID, filetransfer.BatchApprovalRequest{ApprovedItemIDs: approvedIDs})
	if err != nil {
		t.Fatalf("approve download batch: %v", err)
	}
	if err := handlers.validateDownloadBatchBeforeRun(context.Background(), runtime, approvedBatch); err == nil {
		t.Fatal("expected approved download batch to be revalidated against the 1 GiB limit")
	}
	handlers.cleanupBatchTemps(runtime, pendingBatch.ID)

	body, contentType := multipartUploadBody(t, map[string]string{
		"runtime_id":     strconv.FormatInt(payload.Items[0].TransferRuntimeID, 10),
		"remote_dir":     "/daily",
		"overwrite":      "false",
		"relative_paths": `["nested/report.txt"]`,
	}, map[string][]byte{"report.txt": []byte("report payload")})
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/file-transfers/upload-batch", body)
	uploadRequest.Host = "localhost:8080"
	uploadRequest.RemoteAddr = "127.0.0.1:12345"
	uploadRequest.Header.Set("Content-Type", contentType)
	if cookie := currentTestUICookie(); cookie != nil {
		uploadRequest.AddCookie(cookie)
	}
	uploadRequest.AddCookie(&http.Cookie{Name: uiCSRFCookieName, Value: testUICSRFToken})
	uploadRequest.Header.Set(uiCSRFHeaderName, testUICSRFToken)
	upload := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(upload, uploadRequest)
	if upload.Code != http.StatusAccepted {
		t.Fatalf("start S3 upload batch: %d %s", upload.Code, upload.Body.String())
	}
	var uploadBatch struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(upload.Body.Bytes(), &uploadBatch); err != nil || uploadBatch.ID < 1 {
		t.Fatalf("decode S3 upload batch: id=%d err=%v", uploadBatch.ID, err)
	}
	select {
	case objectPath := <-uploadedPath:
		if objectPath != "/test-bucket/daily/nested/report.txt" {
			t.Fatalf("uploaded object path = %q", objectPath)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for S3 upload")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfer-batches/"+strconv.FormatInt(uploadBatch.ID, 10), "", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("read S3 upload batch: %d %s", response.Code, response.Body.String())
		}
		if bytes.Contains(response.Body.Bytes(), []byte(`"status":"completed"`)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("S3 upload batch did not complete: %s", response.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func multipartUploadBody(t *testing.T, fields map[string]string, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write multipart field: %v", err)
		}
	}
	for name, data := range files {
		part, err := writer.CreateFormFile("files", name)
		if err != nil {
			t.Fatalf("create multipart file: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("write multipart file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body, writer.FormDataContentType()
}
