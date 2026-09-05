package api

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestS3LocalActionAPIExactIdentity(t *testing.T) {
	for _, key := range []string{"invoice ", "/invoice", "a//b", "a/../b", "caf\u00e9", "cafe\u0301", " ", "folder/", "control\tkey"} {
		t.Run(key, func(t *testing.T) {
			var mu sync.Mutex
			objects := map[string]bool{key: true, "invoice": true}
			calls := []string{}
			objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				actual := strings.TrimPrefix(r.URL.Path, "/identity-bucket/")
				calls = append(calls, r.Method+" "+actual)
				if !objects[actual] {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.Method == http.MethodDelete {
					delete(objects, actual)
				}
				w.Header().Set("Content-Length", "0")
			}))
			defer objectStore.Close()
			fixture := newAPITestFixture(t)
			target := createS3IdentityRuntime(t, fixture.server, objectStore.URL)
			for _, action := range []string{"get_object_metadata", "download_object", "delete_object"} {
				response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-actions/local-run", "", localConnectorActionRequest{TargetRef: target.Ref, ActionName: action, Input: map[string]any{"key": key}, IdempotencyKey: "identity-" + action})
				var result struct {
					Status string `json:"status"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Status != "completed" {
					t.Fatalf("%s: %d %s", action, response.Code, response.Body.String())
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if objects[key] || !objects["invoice"] {
				t.Fatalf("wrong object deleted: %v", objects)
			}
			for i, method := range []string{"HEAD", "GET", "DELETE"} {
				if calls[i] != method+" "+key {
					t.Fatalf("wire identity = %q", calls[i])
				}
			}
		})
	}
}

func TestS3LocalActionAPIWhitespacePrefix(t *testing.T) {
	objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("prefix") != " " {
				t.Errorf("list prefix = %q", r.URL.Query().Get("prefix"))
			}
			_, _ = w.Write([]byte(`<ListBucketResult/>`))
			return
		}
		var body struct {
			Rule struct {
				Filter struct {
					Prefix string `xml:"Prefix"`
				} `xml:"Filter"`
			} `xml:"Rule"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&body); err != nil || body.Rule.Filter.Prefix != " " {
			t.Errorf("lifecycle prefix changed: %q, %v", body.Rule.Filter.Prefix, err)
		}
	}))
	defer objectStore.Close()
	fixture := newAPITestFixture(t)
	target := createS3IdentityRuntime(t, fixture.server, objectStore.URL)
	for _, action := range []string{"list_objects", "replace_bucket_lifecycle"} {
		input := map[string]any{"prefix": " "}
		if action == "replace_bucket_lifecycle" {
			input["expire_current_after_days"] = 30
		}
		response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-actions/local-run", "", localConnectorActionRequest{TargetRef: target.Ref, ActionName: action, Input: input, IdempotencyKey: "prefix-" + action})
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"completed"`) {
			t.Fatalf("%s: %d %s", action, response.Code, response.Body.String())
		}
	}
}
