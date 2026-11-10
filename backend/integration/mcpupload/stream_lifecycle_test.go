package mcpupload

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestMCPUploadDoesNotPinWorkspaceWhileBodyIsStalled(t *testing.T) {
	fixture, target := newUploadFixture(t)
	token := fixture.token("stalled-upload")
	fixture.grantUpload(target, token.ID, connectortargets.ActionPermissionAlwaysRun)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	multipartWriter := multipart.NewWriter(writer)
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/mcp/connector-actions/upload", reader)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+token.TokenValue)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		fixture.handler.ServeHTTP(response, request)
		finished <- response
	}()
	manifest := uploadTestMetadata(target.TargetRef, "/tmp/test-not-published", "stalled-upload", make([]byte, 128<<10))
	partial := make(chan error, 1)
	go func() { partial <- writePartialUpload(multipartWriter, manifest) }()
	select {
	case err := <-partial:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not consume the partial body")
	}
	locked := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		locked <- fixture.request(http.MethodPost, "/api/lock", "", map[string]string{"scope": "all"})
	}()
	select {
	case response := <-locked:
		if response.Code != http.StatusOK {
			t.Fatalf("lock: %d %s", response.Code, response.Body)
		}
	case <-time.After(5 * time.Second):
		_ = writer.Close()
		t.Fatal("unfinished upload held the workspace lifecycle lock")
	}
	_ = writer.Close()
	select {
	case response := <-finished:
		if response.Code == http.StatusOK {
			t.Fatalf("unfinished upload was accepted: %s", response.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("aborted upload did not release its request")
	}
}

func writePartialUpload(writer *multipart.Writer, manifest map[string]any) error {
	meta, err := writer.CreateFormField("metadata")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(meta).Encode(manifest); err != nil {
		return err
	}
	file, err := writer.CreateFormFile("file", "ignored")
	if err != nil {
		return err
	}
	_, err = file.Write(make([]byte, 64<<10))
	return err
}
