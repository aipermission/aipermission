package s3connector

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestNewS3ClientPropagatesOptionalSessionTokenFailure(t *testing.T) {
	runtime := s3TestRuntime(t, "http://127.0.0.1:9000")
	runtime.Secrets = sessionTokenFailureSecrets{err: errors.New("vault unavailable")}
	if _, err := newS3Client(context.Background(), runtime); err == nil || !strings.Contains(err.Error(), "vault unavailable") {
		t.Fatalf("session token failure = %v", err)
	}
}

func TestPrepareUploadRedactsContentPreview(t *testing.T) {
	connector := New()
	prepared, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     s3TestTarget(t, "http://127.0.0.1:9000"),
		Profile:    s3TestProfile(),
		ActionName: ActionUploadObject,
		Input: map[string]any{
			"key":          "/daily/secret.txt",
			"content_text": "super-secret-content",
			"content_type": "text/plain",
			"overwrite":    true,
		},
		Reason: "unit test",
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Risk != connectors.RiskWrite {
		t.Fatalf("risk = %q", prepared.Risk)
	}
	if prepared.Payload["key"] != "daily/secret.txt" {
		t.Fatalf("key = %#v", prepared.Payload["key"])
	}
	if strings.Contains(strings.Join(mapValues(prepared.Preview), " "), "super-secret-content") {
		t.Fatalf("preview leaked upload content: %#v", prepared.Preview)
	}
	if prepared.Payload["content_base64"] == "" {
		t.Fatalf("payload missing normalized content")
	}
	decoded, err := base64.StdEncoding.DecodeString(prepared.Payload["content_base64"].(string))
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if string(decoded) != "super-secret-content" {
		t.Fatalf("decoded payload = %q", decoded)
	}
}

func TestExecuteListObjectsSignsBoundedRequest(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.String()
		if r.Header.Get("Authorization") == "" {
			t.Fatal("missing authorization header")
		}
		if r.Header.Get("X-Amz-Date") == "" {
			t.Fatal("missing x-amz-date header")
		}
		if r.URL.Path != "/test-bucket" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("list-type") != "2" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<ListBucketResult>
<Name>test-bucket</Name>
<IsTruncated>false</IsTruncated>
<Contents><Key>daily/app.aipdb</Key><LastModified>2026-06-29T10:00:00.000Z</LastModified><ETag>"abc"</ETag><Size>42</Size><StorageClass>STANDARD</StorageClass></Contents>
<Contents><Key>daily/notes.txt</Key><LastModified>2026-06-29T10:01:00.000Z</LastModified><ETag>"def"</ETag><Size>12</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`))
	}))
	defer server.Close()

	connector := New()
	result, err := connector.ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionListObjects,
		Payload:    map[string]any{"prefix": "daily/", "search": "app", "limit": 10},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	output := result.Output.(map[string]any)
	if output["count"] != 1 {
		t.Fatalf("count = %#v output=%#v", output["count"], output)
	}
	objects := output["objects"].([]map[string]any)
	if objects[0]["key"] != "daily/app.aipdb" {
		t.Fatalf("objects = %#v", objects)
	}
	if !strings.Contains(requestedPath, "prefix=daily%2F") {
		t.Fatalf("requested path = %q", requestedPath)
	}
}

func TestExecuteListObjectsSearchScansSubsequentPages(t *testing.T) {
	requests := 0
	var sawCursor bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") == "" {
			t.Fatal("missing authorization header")
		}
		w.Header().Set("Content-Type", "application/xml")
		switch requests {
		case 1:
			_, _ = w.Write([]byte(`<ListBucketResult>
<Name>test-bucket</Name>
<IsTruncated>true</IsTruncated>
<NextContinuationToken>page-2</NextContinuationToken>
<Contents><Key>daily/other.txt</Key><LastModified>2026-06-29T10:00:00.000Z</LastModified><ETag>"abc"</ETag><Size>42</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`))
		case 2:
			sawCursor = r.URL.Query().Get("continuation-token") == "page-2"
			_, _ = w.Write([]byte(`<ListBucketResult>
<Name>test-bucket</Name>
<IsTruncated>false</IsTruncated>
<Contents><Key>assets/project-icon.svg</Key><LastModified>2026-06-29T10:01:00.000Z</LastModified><ETag>"def"</ETag><Size>12</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`))
		default:
			t.Fatalf("unexpected extra request %d", requests)
		}
	}))
	defer server.Close()

	connector := New()
	result, err := connector.ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionListObjects,
		Payload:    map[string]any{"search": "project-icon.svg", "limit": 10},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
	if !sawCursor {
		t.Fatal("second request did not use continuation cursor")
	}
	output := result.Output.(map[string]any)
	if output["count"] != 1 {
		t.Fatalf("count = %#v output=%#v", output["count"], output)
	}
	objects := output["objects"].([]map[string]any)
	if objects[0]["key"] != "assets/project-icon.svg" {
		t.Fatalf("objects = %#v", objects)
	}
}

func TestExecuteListObjectsReturnsDirectoryPrefixes(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.String()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<ListBucketResult>
<Name>test-bucket</Name>
<IsTruncated>false</IsTruncated>
<CommonPrefixes><Prefix>2025/</Prefix></CommonPrefixes>
<CommonPrefixes><Prefix>2026/</Prefix></CommonPrefixes>
<Contents><Key>root.txt</Key><LastModified>2026-06-29T10:00:00.000Z</LastModified><ETag>"abc"</ETag><Size>42</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`))
	}))
	defer server.Close()

	connector := New()
	result, err := connector.ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionListObjects,
		Payload:    map[string]any{"limit": 10},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(requestedPath, "delimiter=%2F") {
		t.Fatalf("requested path = %q", requestedPath)
	}
	output := result.Output.(map[string]any)
	if output["directory_count"] != 2 {
		t.Fatalf("directory_count = %#v output=%#v", output["directory_count"], output)
	}
	directories := output["directories"].([]map[string]any)
	if directories[0]["prefix"] != "2025/" || directories[1]["name"] != "2026" {
		t.Fatalf("directories = %#v", directories)
	}
	browseInput := directories[0]["browse_input"].(map[string]any)
	if browseInput["prefix"] != "2025/" {
		t.Fatalf("browse_input = %#v", browseInput)
	}
	hints := output["assistant_hints"].([]string)
	if len(hints) == 0 || !strings.Contains(strings.Join(hints, " "), "browse_input") {
		t.Fatalf("assistant_hints = %#v", hints)
	}
}

func TestExecuteListObjectsReturnsNextPageInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<ListBucketResult>
<Name>test-bucket</Name>
<IsTruncated>true</IsTruncated>
<NextContinuationToken>page-2</NextContinuationToken>
<Contents><Key>daily/one.txt</Key><LastModified>2026-06-29T10:00:00.000Z</LastModified><ETag>"abc"</ETag><Size>42</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`))
	}))
	defer server.Close()

	connector := New()
	result, err := connector.ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionListObjects,
		Payload:    map[string]any{"prefix": "daily/", "limit": 10},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	output := result.Output.(map[string]any)
	if output["next_cursor"] != "page-2" {
		t.Fatalf("next_cursor = %#v", output["next_cursor"])
	}
	nextPageInput := output["next_page_input"].(map[string]any)
	if nextPageInput["prefix"] != "daily/" || nextPageInput["cursor"] != "page-2" {
		t.Fatalf("next_page_input = %#v", nextPageInput)
	}
	hints := output["assistant_hints"].([]string)
	if len(hints) == 0 || !strings.Contains(strings.Join(hints, " "), "next_cursor") {
		t.Fatalf("assistant_hints = %#v", hints)
	}
}

func TestPrepareListObjectsUsesNonSecretCursorPayload(t *testing.T) {
	connector := New()
	prepared, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     s3TestTarget(t, "http://127.0.0.1:9000"),
		Profile:    s3TestProfile(),
		ActionName: ActionListObjects,
		Input: map[string]any{
			"prefix": "daily/",
			"search": "app",
			"cursor": "opaque-pagination-cursor",
			"limit":  10,
		},
		Reason: "unit test",
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Payload["cursor"] != "opaque-pagination-cursor" {
		t.Fatalf("cursor = %#v", prepared.Payload["cursor"])
	}
	if _, ok := prepared.Payload["continuation_token"]; ok {
		t.Fatalf("payload should not use token-named pagination field: %#v", prepared.Payload)
	}
}

func TestExecuteUploadRejectsExistingObjectWithoutOverwrite(t *testing.T) {
	putCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			putCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	connector := New()
	_, err := connector.ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionUploadObject,
		Payload: map[string]any{
			"key":            "daily/app.txt",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("hello")),
			"content_type":   "text/plain",
			"overwrite":      false,
		},
	})
	if err == nil {
		t.Fatal("expected overwrite guard error")
	}
	if putCalled {
		t.Fatal("put should not run when overwrite guard fails")
	}
}

func TestRenameObjectIsNotExposedAndLegacyRequestsFailClosed(t *testing.T) {
	actions, err := New().GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatalf("get action list: %v", err)
	}
	for _, action := range actions {
		if action.Name == ActionRenameObject {
			t.Fatal("rename_object must not be exposed")
		}
	}
	runtime := s3TestRuntime(t, "http://127.0.0.1:1")
	_, err = New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     runtime.Target,
		Profile:    runtime.Profile,
		ActionName: ActionRenameObject,
		Input: map[string]any{
			"source_key":      "incoming/report.txt",
			"destination_key": "archive/report.txt",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected prepare to reject legacy rename, got %v", err)
	}
	_, err = New().ExecuteAction(context.Background(), connectors.RuntimeContext{}, connectors.PreparedAction{
		ActionName: ActionRenameObject,
		Payload: map[string]any{
			"source_key":      "incoming/report.txt",
			"destination_key": "archive/report.txt",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected execute to reject legacy rename before network access, got %v", err)
	}
}

func TestS3URLPreservesRawPathEscaping(t *testing.T) {
	client := &s3Client{
		scheme:    "https",
		host:      "s3.example.com",
		port:      443,
		bucket:    "my bucket",
		pathStyle: true,
	}
	u := client.URL("folder name/object #1.txt", nil)
	if u.Path != "/my bucket/folder name/object #1.txt" {
		t.Fatalf("path = %q", u.Path)
	}
	if u.EscapedPath() != "/my%20bucket/folder%20name/object%20%231.txt" {
		t.Fatalf("escaped path = %q", u.EscapedPath())
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	canonical, signedHeaders := canonicalRequest(req, emptySHA256Hex)
	if signedHeaders != "host;x-amz-content-sha256;x-amz-date" {
		t.Fatalf("signed headers = %q", signedHeaders)
	}
	if !strings.Contains(canonical, "\nhost:s3.example.com\n") {
		t.Fatalf("canonical request missing url host: %q", canonical)
	}
}

func TestCanonicalRequestSignsConditionalHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodDelete, "https://s3.example.com/bucket/key", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("If-Match", `"etag"`)
	req.Header.Set("If-None-Match", "*")
	canonical, signedHeaders := canonicalRequest(req, emptySHA256Hex)
	if signedHeaders != "host;if-match;if-none-match;x-amz-content-sha256;x-amz-date" {
		t.Fatalf("signed headers = %q", signedHeaders)
	}
	if !strings.Contains(canonical, "if-match:\"etag\"\nif-none-match:*\n") {
		t.Fatalf("canonical request missing conditional headers: %q", canonical)
	}
}

func s3TestRuntime(t *testing.T, rawURL string) connectors.RuntimeContext {
	t.Helper()
	return connectors.RuntimeContext{
		Target:       s3TestTarget(t, rawURL),
		Profile:      s3TestProfile(),
		Secrets:      staticSecrets{"secret_access_key": "test-secret"},
		Capabilities: s3TestCapabilities{},
	}
}

func s3TestTarget(t *testing.T, rawURL string) connectors.TargetView {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split host: %v", err)
	}
	return connectors.TargetView{
		ID:            7,
		Ref:           "s3:7:70",
		ConnectorKind: Kind,
		Name:          "object-store",
		Config: map[string]any{
			"connection_mode": "direct",
			"scheme":          parsed.Scheme,
			"host":            host,
			"port":            portText,
			"region":          "us-east-1",
			"bucket":          "test-bucket",
			"path_style":      true,
		},
	}
}

func s3TestProfile() connectors.CredentialProfileView {
	return connectors.CredentialProfileView{
		ID:            70,
		TargetID:      7,
		ConnectorKind: Kind,
		Kind:          "access_key",
		Label:         "default",
		Public:        map[string]any{"access_key_id": "test-access"},
	}
}

type staticSecrets map[string]string

func (secrets staticSecrets) GetSecret(_ context.Context, name string) (string, error) {
	return secrets[name], nil
}

type sessionTokenFailureSecrets struct{ err error }

func (secrets sessionTokenFailureSecrets) GetSecret(_ context.Context, name string) (string, error) {
	if name == "secret_access_key" {
		return "test-secret", nil
	}
	return "", secrets.err
}

type s3TestCapabilities struct{}

func (s3TestCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return directTransport{}
	}
	return nil
}

type directTransport struct{}

func (directTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (directTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(request.Host, strconv.Itoa(request.Port)))
}

func mapValues(values map[string]any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprint(value))
	}
	return out
}
