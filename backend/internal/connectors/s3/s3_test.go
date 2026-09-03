package s3connector

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestUploadActionDeclaresContentInputsSensitive(t *testing.T) {
	actions, err := New().GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatalf("get action list: %v", err)
	}
	for _, action := range actions {
		if action.Name != ActionUploadObject {
			continue
		}
		joined := strings.Join(action.SensitiveInputFields, ",")
		if !strings.Contains(joined, "content_text") || !strings.Contains(joined, "content_base64") {
			t.Fatalf("sensitive input fields = %#v", action.SensitiveInputFields)
		}
		return
	}
	t.Fatal("upload_object action not found")
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

func TestS3ObjectMutationsSendExpectedETagPreconditions(t *testing.T) {
	tests := []struct {
		name   string
		action connectors.PreparedAction
		method string
	}{
		{
			name: "overwrite",
			action: connectors.PreparedAction{ActionName: ActionUploadObject, Payload: map[string]any{
				"key": "daily/app.txt", "content_base64": base64.StdEncoding.EncodeToString([]byte("hello")), "content_type": "text/plain", "overwrite": true, "expected_etag": "etag-1",
			}},
			method: http.MethodPut,
		},
		{
			name:   "delete",
			action: connectors.PreparedAction{ActionName: ActionDeleteObject, Payload: map[string]any{"key": "daily/app.txt", "expected_etag": `"etag-1"`}},
			method: http.MethodDelete,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != test.method || r.Header.Get("If-Match") != `"etag-1"` {
					t.Fatalf("request method=%s if-match=%q", r.Method, r.Header.Get("If-Match"))
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			if _, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), test.action); err != nil {
				t.Fatalf("execute conditional mutation: %v", err)
			}
		})
	}
}

func TestPrepareUploadRequiresOverwriteForExpectedETag(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target: s3TestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: ActionUploadObject,
		Input: map[string]any{"key": "daily/app.txt", "content_text": "hello", "expected_etag": "etag-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "requires overwrite") {
		t.Fatalf("expected overwrite validation error, got %v", err)
	}
}

func TestPrepareS3MutationRetryPolicyReflectsActualPrecondition(t *testing.T) {
	connector := New()
	tests := []struct {
		name      string
		action    string
		input     map[string]any
		wantClass connectors.RetryClass
	}{
		{name: "unguarded overwrite", action: ActionUploadObject, input: map[string]any{"key": "old.txt", "content_text": "new", "overwrite": true}, wantClass: connectors.RetryNonIdempotent},
		{name: "unguarded delete", action: ActionDeleteObject, input: map[string]any{"key": "old.txt"}, wantClass: connectors.RetryNonIdempotent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
				Target: s3TestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: test.action, Input: test.input,
			})
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			policy := connectors.RetryPolicy{Class: connectors.RetryNonIdempotent}
			if prepared.RetryPolicy != nil {
				policy = connectors.EffectiveRetryPolicy(connectors.ActionDefinition{Risk: prepared.Risk, RetryPolicy: *prepared.RetryPolicy})
			}
			if policy.Class != test.wantClass {
				t.Fatalf("retry policy = %#v, want %q", policy, test.wantClass)
			}
		})
	}
}

func TestPrepareS3ConditionDependentMutationsRequireVerifiedProvider(t *testing.T) {
	tests := []struct {
		name   string
		action string
		input  map[string]any
	}{
		{name: "create if absent", action: ActionUploadObject, input: map[string]any{"key": "new.txt", "content_text": "new"}},
		{name: "guarded overwrite", action: ActionUploadObject, input: map[string]any{"key": "old.txt", "content_text": "new", "overwrite": true, "expected_etag": "etag-1"}},
		{name: "guarded delete", action: ActionDeleteObject, input: map[string]any{"key": "old.txt", "expected_etag": "etag-1"}},
		{name: "presigned create if absent", action: ActionPresignUpload, input: map[string]any{"key": "new.txt"}},
		{name: "version restore", action: ActionRestoreVersion, input: map[string]any{"key": "old.txt", "version_id": "version-1", "expected_current_absent": true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
				Target: s3TestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: test.action, Input: test.input,
			})
			if err == nil || !strings.Contains(err.Error(), "requires verified conditional requests") {
				t.Fatalf("prepare error = %v", err)
			}
		})
	}
}

func TestPrepareS3MutationRetryPolicyRequiresVerifiedProviderSemantics(t *testing.T) {
	target := s3TestTarget(t, "http://127.0.0.1:9000")
	target.Config["trust_conditional_requests"] = true
	connector := New()
	tests := []struct {
		name      string
		action    string
		input     map[string]any
		wantClass connectors.RetryClass
	}{
		{name: "create if absent", action: ActionUploadObject, input: map[string]any{"key": "new.txt", "content_text": "new"}, wantClass: connectors.RetryConditional},
		{name: "guarded overwrite remains non idempotent", action: ActionUploadObject, input: map[string]any{"key": "old.txt", "content_text": "new", "overwrite": true, "expected_etag": "etag-1"}, wantClass: connectors.RetryNonIdempotent},
		{name: "guarded delete", action: ActionDeleteObject, input: map[string]any{"key": "old.txt", "expected_etag": "etag-1"}, wantClass: connectors.RetryConditional},
		{name: "restore if absent", action: ActionRestoreVersion, input: map[string]any{"key": "old.txt", "version_id": "version-1", "expected_current_absent": true}, wantClass: connectors.RetryConditional},
		{name: "restore over current can repeat a version", action: ActionRestoreVersion, input: map[string]any{"key": "old.txt", "version_id": "version-1", "expected_current_etag": "etag-1"}, wantClass: connectors.RetryNonIdempotent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
				Target: target, Profile: s3TestProfile(), ActionName: test.action, Input: test.input,
			})
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			policy := connectors.EffectiveRetryPolicy(connectors.ActionDefinition{Risk: prepared.Risk})
			if prepared.RetryPolicy != nil {
				policy = connectors.EffectiveRetryPolicy(connectors.ActionDefinition{Risk: prepared.Risk, RetryPolicy: *prepared.RetryPolicy})
			}
			if policy.Class != test.wantClass {
				t.Fatalf("retry policy = %#v, want %q", policy, test.wantClass)
			}
		})
	}
}

func TestS3MutationClassifiesUncertainHTTPAndOversizedResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "request timeout", status: http.StatusRequestTimeout, body: "request timed out after dispatch"},
		{name: "server error", status: http.StatusInternalServerError, body: "provider failed after dispatch"},
		{name: "oversized response", status: http.StatusOK, body: strings.Repeat("x", maxS3ResponseBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
				ActionName: ActionDeleteObject,
				Payload:    map[string]any{"key": "daily/app.txt", "expected_etag": "etag-1"},
			})
			if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
				t.Fatalf("error = %v, status = %q", err, connectors.ErrorStatus(err))
			}
		})
	}
}

func TestS3MutationDistinguishesFailuresBeforeAndAfterDispatch(t *testing.T) {
	client := &s3Client{
		scheme: "http", host: "s3.invalid", port: 80, region: "us-east-1", bucket: "bucket",
		accessKey: "access", secretKey: "secret",
		httpClient: &http.Client{Transport: s3RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial refused")
		})},
	}
	err := client.DeleteObject(t.Context(), "object.txt", http.Header{"If-Match": []string{`"etag"`}})
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		t.Fatalf("pre-dispatch error was misclassified: %v", err)
	}

	client.httpClient.Transport = s3RoundTripFunc(func(request *http.Request) (*http.Response, error) {
		trace := httptrace.ContextClientTrace(request.Context())
		trace.WroteRequest(httptrace.WroteRequestInfo{})
		return nil, errors.New("connection reset")
	})
	err = client.DeleteObject(t.Context(), "object.txt", http.Header{"If-Match": []string{`"etag"`}})
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("post-dispatch error was not classified as unknown: %v", err)
	}
}

type s3RoundTripFunc func(*http.Request) (*http.Response, error)

func (function s3RoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestS3MutationClassifiesPreconditionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "etag changed", http.StatusPreconditionFailed)
	}))
	defer server.Close()
	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionDeleteObject,
		Payload:    map[string]any{"key": "daily/app.txt", "expected_etag": "etag-1"},
	})
	if connectors.ErrorCode(err) != "precondition_failed" {
		t.Fatalf("error = %v, code = %q", err, connectors.ErrorCode(err))
	}
}

func TestS3ConditionalMutationClassifiesConcurrentNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "object disappeared during conditional mutation", http.StatusNotFound)
	}))
	defer server.Close()
	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionDeleteObject,
		Payload:    map[string]any{"key": "daily/app.txt", "expected_etag": "etag-1"},
	})
	if connectors.ErrorCode(err) != "precondition_failed" {
		t.Fatalf("conditional error = %v, code = %q", err, connectors.ErrorCode(err))
	}
	_, err = New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionDeleteObject,
		Payload:    map[string]any{"key": "daily/app.txt"},
	})
	if connectors.ErrorCode(err) == "precondition_failed" {
		t.Fatalf("unguarded not-found was misclassified: %v", err)
	}
}

func TestS3ConditionalMutationPreservesNoSuchBucket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchBucket</Code><Message>bucket missing</Message></Error>`))
	}))
	defer server.Close()
	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionDeleteObject,
		Payload:    map[string]any{"key": "daily/app.txt", "expected_etag": "etag-1"},
	})
	if connectors.ErrorCode(err) != "not_found" {
		t.Fatalf("error = %v, code = %q", err, connectors.ErrorCode(err))
	}
}

func TestS3MutationClassifiesTransportFailureAsOutcomeUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		connection, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("hijack connection: %v", err)
		}
		_ = connection.Close()
	}))
	defer server.Close()
	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionDeleteObject,
		Payload:    map[string]any{"key": "daily/app.txt", "expected_etag": "etag-1"},
	})
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
		t.Fatalf("error = %v, code = %q, status = %q", err, connectors.ErrorCode(err), connectors.ErrorStatus(err))
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

func TestSigV4MatchesAWSGetBucketLifecycleReferenceVector(t *testing.T) {
	// Published AWS SigV4 header-auth reference vector:
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html
	client := &s3Client{
		region:    "us-east-1",
		accessKey: "AKIAIOSFODNN7EXAMPLE",
		secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	req, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/?lifecycle", nil)
	if err != nil {
		t.Fatalf("create AWS reference request: %v", err)
	}
	client.signAt(req, nil, time.Date(2013, time.May, 24, 0, 0, 0, 0, time.UTC))

	canonical, signedHeaders := canonicalRequest(req, emptySHA256Hex)
	if signedHeaders != "host;x-amz-content-sha256;x-amz-date" {
		t.Fatalf("signed headers = %q", signedHeaders)
	}
	if got := sha256Hex([]byte(canonical)); got != "9766c798316ff2757b517bc739a67f6213b4ab36dd5da2f94eaebf79c77395ca" {
		t.Fatalf("AWS canonical request hash = %q", got)
	}
	wantAuthorization := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543"
	if got := req.Header.Get("Authorization"); got != wantAuthorization {
		t.Fatalf("AWS authorization header = %q", got)
	}
}

func s3TestRuntime(t *testing.T, rawURL string) connectors.RuntimeContext {
	t.Helper()
	target := s3TestTarget(t, rawURL)
	target.Config["trust_conditional_requests"] = true
	return connectors.RuntimeContext{
		Target:       target,
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

func s3VerifiedTestTarget(t *testing.T, rawURL string) connectors.TargetView {
	t.Helper()
	target := s3TestTarget(t, rawURL)
	target.Config["trust_conditional_requests"] = true
	return target
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
