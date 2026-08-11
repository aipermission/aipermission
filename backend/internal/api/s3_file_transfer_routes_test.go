package api

import (
	"bytes"
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
)

func TestS3ProfileExposesGenericFileTransferRuntime(t *testing.T) {
	uploadedPath := make(chan string, 1)
	objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Fatal("S3 request was not signed")
		}
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("prefix") == "daily/nested/report.txt/" {
				_, _ = w.Write([]byte(`<ListBucketResult/>`))
				return
			}
			_, _ = w.Write([]byte(`<ListBucketResult><Contents><Key>daily/report.txt</Key><Size>12</Size></Contents></ListBucketResult>`))
		case http.MethodHead:
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
				"connection_mode": "direct",
				"scheme":          endpoint.Scheme,
				"host":            host,
				"port":            port,
				"region":          "us-east-1",
				"bucket":          "test-bucket",
				"path_style":      true,
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
	if browse.Code != http.StatusOK || !bytes.Contains(browse.Body.Bytes(), []byte(`"path":"/daily/report.txt"`)) {
		t.Fatalf("browse S3 transfer runtime: %d %s", browse.Code, browse.Body.String())
	}

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
