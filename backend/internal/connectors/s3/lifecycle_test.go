package s3connector

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestExecuteGetBucketLifecycleSummarizesRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if _, ok := r.URL.Query()["lifecycle"]; !ok {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Rule><ID>archive-expiry</ID><Status>Enabled</Status><Filter><Prefix>archive/</Prefix></Filter><Expiration><Days>30</Days></Expiration><NoncurrentVersionExpiration><NoncurrentDays>7</NoncurrentDays></NoncurrentVersionExpiration><AbortIncompleteMultipartUpload><DaysAfterInitiation>3</DaysAfterInitiation></AbortIncompleteMultipartUpload></Rule>
</LifecycleConfiguration>`))
	}))
	defer server.Close()

	result, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{ActionName: ActionGetLifecycle})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	output := result.Output.(map[string]any)
	if output["configured"] != true || output["rule_count"] != 1 {
		t.Fatalf("output = %#v", output)
	}
	rule := output["rules"].([]map[string]any)[0]
	if rule["prefix"] != "archive/" || rule["expire_current_after_days"] != 30 || rule["abort_incomplete_multipart_days"] != 3 {
		t.Fatalf("rule = %#v", rule)
	}
}

func TestExecuteGetBucketLifecycleTreatsMissingPolicyAsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchLifecycleConfiguration</Code></Error>`))
	}))
	defer server.Close()

	result, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{ActionName: ActionGetLifecycle})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	output := result.Output.(map[string]any)
	if output["configured"] != false || output["rule_count"] != 0 {
		t.Fatalf("output = %#v", output)
	}
}

func TestExecuteReplaceBucketLifecycleSendsOneExplicitRule(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s", r.Method)
		}
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		if r.Header.Get("Content-MD5") == "" {
			t.Fatal("missing Content-MD5 header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionReplaceLifecycle,
		Payload: map[string]any{
			"rule_id": "cleanup", "prefix": "tmp/", "expire_current_after_days": 30,
			"expire_noncurrent_after_days": 7, "abort_incomplete_multipart_days": 3, "enabled": true,
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, expected := range []string{
		`<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`,
		`<ID>cleanup</ID>`, `<Prefix>tmp/</Prefix>`, `<Days>30</Days>`, `<NoncurrentDays>7</NoncurrentDays>`, `<DaysAfterInitiation>3</DaysAfterInitiation>`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("body missing %q: %s", expected, body)
		}
	}
}

func TestPrepareLifecycleReplacementIsDestructiveAndBounded(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target: s3TestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: ActionReplaceLifecycle,
		Input: map[string]any{"prefix": "/tmp/", "expire_current_after_days": 30, "abort_incomplete_multipart_days": 7},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Risk != connectors.RiskDestructive || prepared.Payload["rule_id"] != defaultLifecycleRuleID || prepared.Payload["prefix"] != "tmp/" {
		t.Fatalf("prepared = %#v", prepared)
	}

	_, err = New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target: s3TestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: ActionReplaceLifecycle,
		Input: map[string]any{"rule_id": "unsafe", "expire_current_after_days": 0, "abort_incomplete_multipart_days": 0},
	})
	if err == nil {
		t.Fatal("expected empty lifecycle rule rejection")
	}
}

func TestExecuteDeleteBucketLifecycleUsesExplicitSubresource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s", r.Method)
		}
		if _, ok := r.URL.Query()["lifecycle"]; !ok {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if _, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{ActionName: ActionDeleteLifecycle}); err != nil {
		t.Fatalf("execute: %v", err)
	}
}
