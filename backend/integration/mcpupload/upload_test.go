package mcpupload

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestMCPUploadPromptAndAlwaysThroughSFTP(t *testing.T) {
	fixture, target := newUploadFixture(t)
	token := fixture.token("upload-token")
	other := fixture.token("other-upload-token")
	fixture.grantUpload(target, other.ID, "")
	fixture.grantUpload(target, token.ID, connectortargets.ActionPermissionApprovalRequired)
	verifyUploadDiscovery(t, fixture.handler, target.TargetRef, token.TokenValue)
	destination := filepath.Join(t.TempDir(), "artifact.bin")
	handler := fixture.handler
	content := bytes.Repeat([]byte("binary\x00artifact\xff"), 1<<17)
	metadata := uploadTestMetadata(target.TargetRef, destination, "prompt-upload", content)
	initial := sendUploadFixture(t, handler, token.TokenValue, metadata, content, false)
	accepted := requireUploadAction(t, initial, connectors.ResultApprovalPending)
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("prompt published destination before approval: %v", err)
	}
	replay := requireUploadAction(t, sendUploadFixture(t, handler, token.TokenValue, metadata, content, false), connectors.ResultApprovalPending)
	if !replay.Replayed || replay.RequestID != accepted.RequestID {
		t.Fatalf("unexpected replay: %#v", replay)
	}
	status := requireUploadAction(t, uploadStatusFixture(handler, token.TokenValue, target.TargetRef, "prompt-upload"), connectors.ResultCompleted)
	if status.Output.(map[string]any)["action_request_id"] != float64(accepted.RequestID) {
		t.Fatalf("upload status could not reconcile approval: %#v", status.Output)
	}
	approval := fixture.approve(accepted.RequestID)
	if approval.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", approval.Code, approval.Body)
	}
	requireUploadTransfer(t, fixture, handler, token.TokenValue, target.TargetRef, "prompt-upload", "completed")
	if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("published upload is incorrect: length=%d err=%v", len(got), err)
	}
	foreign := uploadStatusFixture(handler, other.TokenValue, target.TargetRef, "prompt-upload")
	requireUploadAction(t, foreign, connectors.ResultFailed)

	fixture.grantUpload(target, token.ID, connectortargets.ActionPermissionAlwaysRun)
	overwrite := uploadTestMetadata(target.TargetRef, destination, "existing-no-overwrite", nil)
	requireUploadAction(t, sendUploadFixture(t, handler, token.TokenValue, overwrite, nil, false), connectors.ResultCompleted)
	requireUploadTransfer(t, fixture, handler, token.TokenValue, target.TargetRef, "existing-no-overwrite", "failed")
	if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, content) {
		t.Fatal("overwrite=false changed the existing destination")
	}
	emptyPath := filepath.Join(filepath.Dir(destination), "empty")
	empty := uploadTestMetadata(target.TargetRef, emptyPath, "empty-upload", nil)
	requireUploadAction(t, sendUploadFixture(t, handler, token.TokenValue, empty, nil, false), connectors.ResultCompleted)
	requireUploadTransfer(t, fixture, handler, token.TokenValue, target.TargetRef, "empty-upload", "completed")
	if stat, err := os.Stat(emptyPath); err != nil || stat.Size() != 0 {
		t.Fatalf("empty upload: %v", err)
	}
}

func verifyUploadDiscovery(t *testing.T, handler http.Handler, target, token string) {
	t.Helper()
	endpoint := "/api/mcp/connector-actions?target_ref=" + url.QueryEscape(target)
	for _, query := range []string{"", "&upload=stream-v1"} {
		result := performJSON(handler, http.MethodGet, endpoint+query, token, nil)
		if result.Code != http.StatusOK || strings.Contains(result.Body.String(), `"max_upload_bytes"`) != (query != "") ||
			!strings.Contains(result.Body.String(), `"name":"get_file_upload_status"`) {
			t.Fatalf("action discovery %q: %d %s", query, result.Code, result.Body)
		}
	}
}

func TestMCPUploadCannotRunAfterPermissionRemoved(t *testing.T) {
	fixture, target := newUploadFixture(t)
	token := fixture.token("revoked-upload")
	fixture.grantUpload(target, token.ID, connectortargets.ActionPermissionApprovalRequired)
	destination := filepath.Join(t.TempDir(), "artifact.bin")
	data := []byte("test")
	meta := uploadTestMetadata(target.TargetRef, destination, "revoked-upload", data)
	action := requireUploadAction(t, sendUploadFixture(t, fixture.handler, token.TokenValue, meta, data, false), connectors.ResultApprovalPending)
	fixture.grantUpload(target, token.ID, "")
	if response := fixture.approve(action.RequestID); response.Code != http.StatusConflict && response.Code != http.StatusBadRequest {
		t.Fatalf("revoked approval was not rejected: %d %s", response.Code, response.Body)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("revoked upload published: %v", err)
	}
}

func TestMCPUploadRejectsUnstagedAndMalformedInputs(t *testing.T) {
	fixture, target := newUploadFixture(t)
	token := fixture.token("upload-invalid")
	handler := fixture.handler
	metadata := uploadTestMetadata(target.TargetRef, "/tmp/example", "invalid-upload", []byte("abc"))
	if denied := sendUploadFixture(t, handler, token.TokenValue, metadata, []byte("abc"), false); denied.Code != http.StatusNotFound {
		t.Fatalf("missing permission: %d %s", denied.Code, denied.Body)
	}
	fixture.grantUpload(target, token.ID, connectortargets.ActionPermissionAlwaysRun)
	for _, input := range []struct {
		content []byte
		extra   bool
	}{{[]byte("xyz"), false}, {[]byte("ab"), false}, {[]byte("abcd"), false}, {[]byte("abc"), true}} {
		result := sendUploadFixture(t, handler, token.TokenValue, metadata, input.content, input.extra)
		if result.Code != http.StatusBadRequest {
			t.Fatalf("invalid stream: %d %s", result.Code, result.Body)
		}
	}
	invalidDestination := uploadTestMetadata(target.TargetRef, "relative/example", "invalid-path", []byte("abc"))
	if result := sendUploadFixture(t, handler, token.TokenValue, invalidDestination, []byte("abc"), false); result.Code != http.StatusBadRequest {
		t.Fatalf("invalid destination: %d %s", result.Code, result.Body)
	}
	fake := performJSON(handler, http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, map[string]any{
		"target_ref": target.TargetRef, "action_name": "upload_file", "reason": "must stage", "idempotency_key": "invalid-upload",
		"input": map[string]any{"upload_ref": "invalid-upload", "remote_path": "/tmp/example", "file_name": "file", "size_bytes": 3, "checksum_sha256": metadata["manifest"].(connectors.FileUploadManifest).ChecksumSHA256},
	})
	if fake.Code != http.StatusBadRequest || !strings.Contains(fake.Body.String(), "use upload_connector_file") {
		t.Fatalf("ordinary action submission: %d %s", fake.Code, fake.Body)
	}
	if transfers := fixture.request(http.MethodGet, "/api/file-transfers", "", nil); transfers.Code != 200 || !strings.Contains(transfers.Body.String(), `"total":0`) {
		t.Fatalf("invalid uploads persisted: %d %s", transfers.Code, transfers.Body)
	}
}

func uploadTestMetadata(target, destination, key string, content []byte) map[string]any {
	digest := sha256.Sum256(content)
	return map[string]any{
		"target_ref": target, "action_name": "upload_file", "idempotency_key": key, "reason": "verify a test upload",
		"manifest": connectors.FileUploadManifest{FileName: "artifact.bin", RemotePath: destination, SizeBytes: int64(len(content)), ChecksumSHA256: hex.EncodeToString(digest[:])},
	}
}

func sendUploadFixture(t *testing.T, handler http.Handler, token string, metadata map[string]any, content []byte, extra bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	manifest, err := writer.CreateFormField("metadata")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(manifest).Encode(metadata); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("file", "ignored-local-name")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if extra {
		_ = writer.WriteField("extra", "reject")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/mcp/connector-actions/upload", &body)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requireUploadAction(t *testing.T, response *httptest.ResponseRecorder, status connectors.ResultStatus) actions.Response {
	t.Helper()
	var result actions.Response
	err := json.Unmarshal(response.Body.Bytes(), &result)
	if err != nil || response.Code != http.StatusOK || result.Status != string(status) {
		t.Fatalf("expected action %s, got %d %s (%v)", status, response.Code, response.Body, err)
	}
	return result
}

func uploadStatusFixture(handler http.Handler, token, target, key string) *httptest.ResponseRecorder {
	return performJSON(handler, http.MethodPost, "/api/mcp/connector-actions/call", token, map[string]any{
		"target_ref": target, "action_name": "get_file_upload_status", "input": map[string]any{"upload_ref": key},
	})
}

func requireUploadTransfer(t *testing.T, fixture *uploadFixture, handler http.Handler, token, target, key, expected string) {
	t.Helper()
	var status string
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		result := requireUploadAction(t, uploadStatusFixture(handler, token, target, key), connectors.ResultCompleted)
		output, _ := result.Output.(map[string]any)
		status, _ = output["status"].(string)
		if status == expected {
			return
		}
		if status == "failed" || status == "canceled" {
			transfers := fixture.request(http.MethodGet, "/api/file-transfers", "", nil)
			t.Fatalf("upload %s: %s", status, transfers.Body)
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("upload status=%q, want %q", status, expected)
}
