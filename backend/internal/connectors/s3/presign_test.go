package s3connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestPresignObjectCreatesBoundedSigV4URL(t *testing.T) {
	client := &s3Client{
		scheme:       "https",
		host:         "s3.example.com",
		port:         443,
		region:       "eu-central-1",
		bucket:       "test bucket",
		pathStyle:    true,
		accessKey:    "test-access",
		secretKey:    "test-secret",
		sessionToken: "temporary-session-token",
	}
	now := time.Date(2026, time.August, 11, 10, 30, 0, 0, time.UTC)

	signedURL, expiresAt, err := client.PresignObject(http.MethodGet, "folder name/object #1.txt", 900, now)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	parsed, err := url.Parse(signedURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if parsed.EscapedPath() != "/test%20bucket/folder%20name/object%20%231.txt" {
		t.Fatalf("escaped path = %q", parsed.EscapedPath())
	}
	query := parsed.Query()
	if query.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || query.Get("X-Amz-Expires") != "900" {
		t.Fatalf("signature query = %#v", query)
	}
	if query.Get("X-Amz-Credential") != "test-access/20260811/eu-central-1/s3/aws4_request" {
		t.Fatalf("credential = %q", query.Get("X-Amz-Credential"))
	}
	if query.Get("X-Amz-Security-Token") != "temporary-session-token" {
		t.Fatalf("session token = %q", query.Get("X-Amz-Security-Token"))
	}
	if query.Get("X-Amz-Signature") != "4f6f6cdf829a73b7037a454fe91d4997c623396975104c73208d33bdc4c096c8" {
		t.Fatalf("signature = %q", query.Get("X-Amz-Signature"))
	}
	if strings.Contains(signedURL, "test-secret") {
		t.Fatal("signed URL leaked the secret access key")
	}
	if !expiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expires at = %s", expiresAt)
	}
}

func TestPreparePresignActionsUsesBoundedRisk(t *testing.T) {
	connector := New()
	download, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     s3TestTarget(t, "http://127.0.0.1:9000"),
		Profile:    s3TestProfile(),
		ActionName: ActionPresignDownload,
		Input:      map[string]any{"key": "/reports/today.csv", "expires_seconds": 99999},
	})
	if err != nil {
		t.Fatalf("prepare download: %v", err)
	}
	if download.Risk != connectors.RiskRead || download.Payload["expires_seconds"] != maxPresignedExpirySeconds {
		t.Fatalf("download = %#v", download)
	}

	upload, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     s3TestTarget(t, "http://127.0.0.1:9000"),
		Profile:    s3TestProfile(),
		ActionName: ActionPresignUpload,
		Input:      map[string]any{"key": "incoming/report.csv"},
	})
	if err != nil {
		t.Fatalf("prepare upload: %v", err)
	}
	if upload.Risk != connectors.RiskWrite || upload.Payload["expires_seconds"] != defaultPresignedExpirySeconds {
		t.Fatalf("upload = %#v", upload)
	}
	if upload.Payload["overwrite"] != false {
		t.Fatalf("overwrite = %#v", upload.Payload["overwrite"])
	}
}

func TestExecutePresignDownloadRequiresExistingObject(t *testing.T) {
	headCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headCalls++
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionPresignDownload,
		Payload:    map[string]any{"key": "missing.txt", "expires_seconds": 900},
	})
	if err == nil || headCalls != 1 {
		t.Fatalf("err = %v head calls = %d", err, headCalls)
	}
}

func TestExecutePresignUploadProtectsExistingObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionPresignUpload,
		Payload:    map[string]any{"key": "existing.txt", "expires_seconds": 900, "overwrite": false},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecutePresignUploadSignsRequiredNoOverwriteHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionPresignUpload,
		Payload:    map[string]any{"key": "/new.txt", "expires_seconds": 900, "overwrite": false},
	})
	if err != nil {
		t.Fatalf("execute presign upload: %v", err)
	}
	output := result.Output.(map[string]any)
	parsed, err := url.Parse(output["url"].(string))
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if parsed.Query().Get("X-Amz-SignedHeaders") != "host;if-none-match" {
		t.Fatalf("signed headers = %q", parsed.Query().Get("X-Amz-SignedHeaders"))
	}
	headers, ok := output["required_headers"].(map[string]string)
	if !ok || headers["If-None-Match"] != "*" {
		t.Fatalf("required headers = %#v", output["required_headers"])
	}
}
