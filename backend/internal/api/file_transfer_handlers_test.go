package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestFileTransferRoutesReturnNotFoundForUnknownRuntime(t *testing.T) {
	fixture := newAPITestFixture(t)
	browse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/browse", "", browseRemoteFilesRequest{
		RuntimeID: 999999,
		Path:      "/",
	})
	if browse.Code != http.StatusNotFound {
		t.Fatalf("browse status = %d body=%s", browse.Code, browse.Body.String())
	}
	download := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download", "", startDownloadRequest{
		RuntimeID:      999999,
		RemotePath:     "/missing.txt",
		IdempotencyKey: "unknown-runtime-download",
	})
	if download.Code != http.StatusNotFound {
		t.Fatalf("download status = %d body=%s", download.Code, download.Body.String())
	}
}

func TestFileTransferStartRoutesRequireBoundedIdempotencyKeys(t *testing.T) {
	fixture := newAPITestFixture(t)
	missing := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download", "", startDownloadRequest{
		RuntimeID: 1, RemotePath: "/tmp/result.txt",
	})
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "idempotency_key is required") {
		t.Fatalf("missing idempotency key: status=%d body=%s", missing.Code, missing.Body.String())
	}
	oversized := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download", "", startDownloadRequest{
		RuntimeID: 1, RemotePath: "/tmp/result.txt", IdempotencyKey: strings.Repeat("x", filetransfer.MaxIdempotencyKeyBytes+1),
	})
	if oversized.Code != http.StatusBadRequest || !strings.Contains(oversized.Body.String(), "idempotency_key is too long") {
		t.Fatalf("oversized idempotency key: status=%d body=%s", oversized.Code, oversized.Body.String())
	}
}
