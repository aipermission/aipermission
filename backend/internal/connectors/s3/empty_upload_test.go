package s3connector

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestPrepareUploadContentPresence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content map[string]any
		want    string
		invalid bool
	}{
		{"empty text", map[string]any{"content_text": ""}, "", false},
		{"empty base64", map[string]any{"content_base64": ""}, "", false},
		{"text", map[string]any{"content_text": "hello"}, "hello", false},
		{"base64", map[string]any{"content_base64": "eA=="}, "x", false},
		{"missing", nil, "", true},
		{"both empty", map[string]any{"content_text": "", "content_base64": ""}, "", true},
		{"both mixed", map[string]any{"content_text": "x", "content_base64": ""}, "", true},
		{"invalid base64", map[string]any{"content_base64": "!"}, "", true},
		{"null", map[string]any{"content_text": nil}, "", true},
		{"number", map[string]any{"content_base64": 0}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]any{"key": "empty.txt"}
			for key, value := range tc.content {
				input[key] = value
			}
			target := s3TestTarget(t, "http://127.0.0.1:9000")
			target.Config["trust_conditional_requests"] = true
			prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
				Target: target, Profile: s3TestProfile(),
				ActionName: ActionUploadObject, Input: input, Reason: "test upload presence",
			})
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid content accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := base64.StdEncoding.DecodeString(prepared.Payload["content_base64"].(string))
			if err != nil || string(data) != tc.want || prepared.Payload["content_bytes"] != len(tc.want) {
				t.Fatalf("normalized content=%q bytes=%v err=%v", data, prepared.Payload["content_bytes"], err)
			}
		})
	}
}

func TestExecuteEmptyUploadPreservesConditionalPut(t *testing.T) {
	for _, field := range []string{"content_text", "content_base64"} {
		t.Run(field, func(t *testing.T) {
			puts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				body, err := io.ReadAll(r.Body)
				if r.Method != http.MethodPut || r.URL.Path != "/test-bucket/empty.txt" || err != nil || len(body) != 0 {
					t.Errorf("empty PUT: method=%s path=%s bytes=%d err=%v", r.Method, r.URL.Path, len(body), err)
				}
				if r.Header.Get("If-None-Match") != "*" || r.Header.Get("Authorization") == "" {
					t.Error("missing signed no-overwrite protection")
				}
				puts++
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			runtime := s3TestRuntime(t, server.URL)
			connector := New()
			prepared, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
				Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionUploadObject,
				Input: map[string]any{"key": "empty.txt", field: ""}, Reason: "test empty PUT",
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := connector.ExecuteAction(context.Background(), runtime, prepared)
			if err != nil || result.Status != connectors.ResultCompleted || puts != 1 {
				t.Fatalf("result=%#v puts=%d err=%v", result, puts, err)
			}
			if result.Output.(map[string]any)["bytes"] != 0 {
				t.Fatalf("nonempty upload output: %#v", result.Output)
			}
		})
	}
}
