package filetransferhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
)

func TestTransferStartUsesOneAcceptedExecutionSnapshot(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		invoke func(*testing.T, transferTestFixture) *httptest.ResponseRecorder
	}{
		{name: "upload", invoke: invokeTestUpload},
		{name: "download", invoke: invokeTestDownload},
		{name: "upload batch", invoke: invokeTestUploadBatch},
		{name: "download batch", invoke: invokeTestDownloadBatch},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newTransferTestFixture(t)
			fixture.handlers.scope = func(http.ResponseWriter) (*Runtime, bool) {
				return fixture.runtime, true
			}
			original := fixture.runtime.connectorPorts
			var resolutions atomic.Int32
			fixture.runtime.connectorPorts = func(ctx context.Context, runtimeID int64) (ConnectorPorts, error) {
				resolutions.Add(1)
				return original(ctx, runtimeID)
			}

			response := testCase.invoke(t, fixture)
			if response.Code != http.StatusAccepted {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if got := resolutions.Load(); got != 1 {
				t.Fatalf("execution resolutions = %d, want 1", got)
			}
			if !fixture.runtime.jobs.Wait(t.Context()) {
				t.Fatal("transfer runner did not drain")
			}
		})
	}
}

func TestTransferBrowseUsesOneAcceptedExecutionSnapshot(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		input   func(int64) any
		handler func(*Handlers, http.ResponseWriter, *http.Request)
	}{
		{
			name: "browse",
			input: func(runtimeID int64) any {
				return browseRemoteFilesRequest{RuntimeID: runtimeID, Path: "/"}
			},
			handler: func(handlers *Handlers, response http.ResponseWriter, request *http.Request) {
				handlers.BrowseRemoteFiles(response, request)
			},
		},
		{
			name: "recursive expansion",
			input: func(runtimeID int64) any {
				return expandRemoteFilesRequest{RuntimeID: runtimeID, Path: "/"}
			},
			handler: func(handlers *Handlers, response http.ResponseWriter, request *http.Request) {
				handlers.ExpandRemoteFiles(response, request)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newTransferTestFixtureWithAdapter(t, successfulBrowseTransferAdapter{})
			fixture.handlers.scope = func(http.ResponseWriter) (*Runtime, bool) {
				return fixture.runtime, true
			}
			original := fixture.runtime.connectorPorts
			var resolutions atomic.Int32
			fixture.runtime.connectorPorts = func(ctx context.Context, runtimeID int64) (ConnectorPorts, error) {
				resolutions.Add(1)
				return original(ctx, runtimeID)
			}

			body, err := json.Marshal(testCase.input(fixture.runtimeID))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			testCase.handler(fixture.handlers, response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if got := resolutions.Load(); got != 1 {
				t.Fatalf("execution resolutions = %d, want 1", got)
			}
		})
	}
}

type successfulBrowseTransferAdapter struct{ rejectingTransferAdapter }

func (successfulBrowseTransferAdapter) BrowseRemoteFiles(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string) ([]connectorapi.RemoteFileEntry, error) {
	return []connectorapi.RemoteFileEntry{}, nil
}

func (successfulBrowseTransferAdapter) ListRecursiveFiles(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string, int, int64, int64) ([]connectorapi.RemoteFileEntry, error) {
	return []connectorapi.RemoteFileEntry{}, nil
}

func invokeTestUpload(t *testing.T, fixture transferTestFixture) *httptest.ResponseRecorder {
	t.Helper()
	request := newMultipartTransferRequest(t, fixture.runtimeID, "/upload.txt", "")
	response := httptest.NewRecorder()
	fixture.handlers.StartUpload(response, request)
	return response
}

func invokeTestUploadBatch(t *testing.T, fixture transferTestFixture) *httptest.ResponseRecorder {
	t.Helper()
	request := newMultipartTransferRequest(t, fixture.runtimeID, "", "/uploads")
	response := httptest.NewRecorder()
	fixture.handlers.StartUploadBatch(response, request)
	return response
}

func newMultipartTransferRequest(t *testing.T, runtimeID int64, remotePath string, remoteDir string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"runtime_id":      strconv.FormatInt(runtimeID, 10),
		"remote_path":     remotePath,
		"remote_dir":      remoteDir,
		"idempotency_key": fmt.Sprintf("test-%s-%d", remotePath+remoteDir, runtimeID),
		"overwrite":       "true",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("file", "upload.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("fixture upload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func invokeTestDownload(t *testing.T, fixture transferTestFixture) *httptest.ResponseRecorder {
	t.Helper()
	return invokeJSONTransferStart(t, fixture, startDownloadRequest{
		RuntimeID: fixture.runtimeID, RemotePath: "/download.txt", IdempotencyKey: "test-download",
	}, fixture.handlers.StartDownload)
}

func invokeTestDownloadBatch(t *testing.T, fixture transferTestFixture) *httptest.ResponseRecorder {
	t.Helper()
	return invokeJSONTransferStart(t, fixture, startDownloadBatchRequest{
		RuntimeID: fixture.runtimeID, RemotePaths: []string{"/download.txt"}, IdempotencyKey: "test-download-batch",
	}, fixture.handlers.StartDownloadBatch)
}

func invokeJSONTransferStart(t *testing.T, fixture transferTestFixture, input any, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}
