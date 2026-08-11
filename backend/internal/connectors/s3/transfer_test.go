package s3connector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBrowseRemoteFilesReturnsVirtualDirectoriesAndObjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("delimiter") != "/" || r.URL.Query().Get("prefix") != "daily/" {
			t.Fatalf("unexpected browse query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`<ListBucketResult>
<CommonPrefixes><Prefix>daily/archive/</Prefix></CommonPrefixes>
<Contents><Key>daily/report.json</Key><LastModified>2026-08-11T10:00:00Z</LastModified><Size>42</Size></Contents>
</ListBucketResult>`))
	}))
	defer server.Close()

	entries, err := BrowseRemoteFiles(context.Background(), s3TestRuntime(t, server.URL), "/daily")
	if err != nil {
		t.Fatalf("browse remote files: %v", err)
	}
	if len(entries) != 2 || entries[0].Type != "directory" || entries[0].Path != "/daily/archive/" || entries[1].Path != "/daily/report.json" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestUploadFileUsesMultipartAndReportsProgress(t *testing.T) {
	partCount := 0
	completed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Fatal("request was not signed")
		}
		switch {
		case r.Method == http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
			_, _ = w.Write([]byte(`<InitiateMultipartUploadResult><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`))
		case r.Method == http.MethodPut && r.URL.Query().Get("uploadId") == "upload-1":
			partCount++
			w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, partCount))
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Query().Get("uploadId") == "upload-1":
			completed = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected multipart request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	fileName := t.TempDir() + "/large.bin"
	data := []byte(strings.Repeat("x", multipartThreshold+1))
	if err := os.WriteFile(fileName, data, 0o600); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}
	var progress int64
	result, err := UploadFile(context.Background(), s3TestRuntime(t, server.URL), fileName, "/archive/large.bin", false, TransferOptions{
		Progress: func(transferred int64, _ int64) { progress = transferred },
	})
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if partCount != 3 || !completed || progress != int64(len(data)) || result.Bytes != int64(len(data)) || result.ChecksumSHA256 == "" {
		t.Fatalf("unexpected multipart result: parts=%d completed=%t progress=%d result=%#v", partCount, completed, progress, result)
	}
}

func TestListRecursiveFilesEnforcesObjectLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`<ListBucketResult>
<Contents><Key>daily/a.txt</Key><Size>10</Size></Contents>
<Contents><Key>daily/b.txt</Key><Size>20</Size></Contents>
</ListBucketResult>`))
		case http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request: %s", r.Method)
		}
	}))
	defer server.Close()

	_, err := ListRecursiveFiles(context.Background(), s3TestRuntime(t, server.URL), "/daily", 1, 100)
	if err == nil || !strings.Contains(err.Error(), "object limit") {
		t.Fatalf("expected object limit error, got %v", err)
	}
}

func TestMultipartUploadDoesNotRetryPermissionFailures(t *testing.T) {
	partAttempts := 0
	aborted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
			_, _ = w.Write([]byte(`<InitiateMultipartUploadResult><UploadId>upload-denied</UploadId></InitiateMultipartUploadResult>`))
		case r.Method == http.MethodPut && r.URL.Query().Get("uploadId") == "upload-denied":
			partAttempts++
			w.WriteHeader(http.StatusForbidden)
		case r.Method == http.MethodDelete && r.URL.Query().Get("uploadId") == "upload-denied":
			aborted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected multipart request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	fileName := t.TempDir() + "/large.bin"
	if err := os.WriteFile(fileName, []byte(strings.Repeat("x", multipartThreshold+1)), 0o600); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}
	_, err := UploadFile(context.Background(), s3TestRuntime(t, server.URL), fileName, "/large.bin", true, TransferOptions{})
	if err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected permission failure, got %v", err)
	}
	if partAttempts != 1 || !aborted {
		t.Fatalf("permission failure attempts=%d aborted=%t", partAttempts, aborted)
	}
}

func TestMultipartUploadRetriesServerFailure(t *testing.T) {
	partAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
			_, _ = w.Write([]byte(`<InitiateMultipartUploadResult><UploadId>upload-retry</UploadId></InitiateMultipartUploadResult>`))
		case r.Method == http.MethodPut && r.URL.Query().Get("uploadId") == "upload-retry":
			partAttempts++
			if partAttempts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("ETag", fmt.Sprintf(`"part-%s"`, r.URL.Query().Get("partNumber")))
		case r.Method == http.MethodPost && r.URL.Query().Get("uploadId") == "upload-retry":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected multipart request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	fileName := t.TempDir() + "/large.bin"
	if err := os.WriteFile(fileName, []byte(strings.Repeat("x", multipartThreshold+1)), 0o600); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}
	if _, err := UploadFile(context.Background(), s3TestRuntime(t, server.URL), fileName, "/large.bin", true, TransferOptions{}); err != nil {
		t.Fatalf("upload after retry: %v", err)
	}
	if partAttempts != 4 {
		t.Fatalf("expected one retry plus three uploaded parts, got %d attempts", partAttempts)
	}
}
