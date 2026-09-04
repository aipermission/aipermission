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
		scheme:    "https",
		host:      "s3.example.com",
		port:      443,
		region:    "eu-central-1",
		bucket:    "test bucket",
		pathStyle: true,
		accessKey: "test-access",
		secretKey: "test-secret",
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
	if query.Get("X-Amz-Security-Token") != "" {
		t.Fatalf("unexpected session token = %q", query.Get("X-Amz-Security-Token"))
	}
	if query.Get("X-Amz-Signature") == "" {
		t.Fatalf("signature = %q", query.Get("X-Amz-Signature"))
	}
	if strings.Contains(signedURL, "test-secret") {
		t.Fatal("signed URL leaked the secret access key")
	}
	if !expiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expires at = %s", expiresAt)
	}
}

func TestPresignObjectRejectsTemporarySessionCredentials(t *testing.T) {
	client := &s3Client{
		scheme: "https", host: "s3.example.com", port: 443, region: "eu-central-1",
		bucket: "test-bucket", accessKey: "temporary-access", secretKey: "temporary-secret",
		sessionToken: "temporary-session-token",
	}
	if _, _, err := client.PresignObject(http.MethodGet, "report.csv", 900, time.Now()); err == nil || !strings.Contains(err.Error(), "session token") {
		t.Fatalf("temporary-session presign error = %v", err)
	}
}

func TestPresignObjectMatchesAWSReferenceVector(t *testing.T) {
	client := &s3Client{
		scheme: "https", host: "s3.amazonaws.com", port: 443,
		region: "us-east-1", bucket: "examplebucket", pathStyle: false,
		accessKey: "AKIAIOSFODNN7EXAMPLE",
		secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	now := time.Date(2013, time.May, 24, 0, 0, 0, 0, time.UTC)
	signedURL, _, err := client.PresignObject(http.MethodGet, "test.txt", 86400, now)
	if err == nil || !strings.Contains(err.Error(), "between") {
		t.Fatalf("public expiry guard should reject the 24-hour AWS vector, url=%q err=%v", signedURL, err)
	}

	// Exercise the unchecked signing primitive because AIPermission deliberately
	// restricts public presigned URLs to one hour while AWS's published SigV4
	// reference vector uses 24 hours:
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-query-string-auth.html
	signedURL, _, err = client.buildPresignedObjectUnchecked(http.MethodGet, "test.txt", 86400, nil, now)
	if err != nil {
		t.Fatalf("presign bounded reference request: %v", err)
	}
	want := "https://examplebucket.s3.amazonaws.com/test.txt?" +
		"X-Amz-Algorithm=AWS4-HMAC-SHA256&" +
		"X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request&" +
		"X-Amz-Date=20130524T000000Z&" +
		"X-Amz-Expires=86400&" +
		"X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404&" +
		"X-Amz-SignedHeaders=host"
	if signedURL != want {
		t.Fatalf("AWS presigned URL\n got: %s\nwant: %s", signedURL, want)
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
		Target:     s3VerifiedTestTarget(t, "http://127.0.0.1:9000"),
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
