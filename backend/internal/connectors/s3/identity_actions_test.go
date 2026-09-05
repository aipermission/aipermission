package s3connector

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestExactIdentityPresignAndVersions(t *testing.T) {
	versionID := " version +/? "
	for _, key := range identityKeys {
		t.Run(key, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("versions") {
					if r.URL.Query().Get("prefix") != key {
						t.Errorf("version prefix = %q", r.URL.Query().Get("prefix"))
					}
					_ = xml.NewEncoder(w).Encode(s3VersionListResult{Versions: []s3ObjectVersion{{Key: key, VersionID: versionID}, {Key: key + "other", VersionID: "v2"}}})
					return
				}
				if r.URL.Path != "/test-bucket/"+key {
					t.Errorf("object path = %q", r.URL.Path)
				}
				if r.Method == http.MethodPut {
					want := "/test-bucket/" + awsPathEscape(key) + "?versionId=" + awsQueryEscape(versionID)
					if r.Header.Get("X-Amz-Copy-Source") != want {
						t.Errorf("copy source = %q", r.Header.Get("X-Amz-Copy-Source"))
					}
					_, _ = w.Write([]byte(`<CopyObjectResult><ETag>"restored"</ETag></CopyObjectResult>`))
				}
				if r.Method == http.MethodDelete && r.URL.Query().Get("versionId") != versionID {
					t.Error("missing version identity")
				}
			}))
			defer server.Close()
			runtime := s3TestRuntime(t, server.URL)
			runtime.Target.Config["trust_conditional_requests"] = true
			for _, name := range []string{ActionPresignDownload, ActionPresignUpload, ActionListVersions, ActionRestoreVersion, ActionDeleteVersion} {
				input := map[string]any{"key": key, "overwrite": true, "version_id": versionID, "expected_current_absent": true}
				prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: name, Input: input})
				if err != nil {
					t.Fatal(err)
				}
				result, err := New().ExecuteAction(context.Background(), runtime, prepared)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				output := result.Output.(map[string]any)
				if name == ActionPresignDownload || name == ActionPresignUpload {
					u, err := url.Parse(output["url"].(string))
					if err != nil || u.Path != "/test-bucket/"+key || u.Query().Get("X-Amz-Signature") == "" {
						t.Fatalf("presigned identity: %v", output)
					}
				}
				if name == ActionListVersions && output["count"] != 1 {
					t.Fatalf("version collision: %v", output)
				}
			}
		})
	}
}

func TestExactPrefixPreparationAndExecution(t *testing.T) {
	prefix := " /a//../ "
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("prefix") != prefix {
				t.Errorf("prefix = %q", r.URL.Query().Get("prefix"))
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
		if err := xml.NewDecoder(r.Body).Decode(&body); err != nil || body.Rule.Filter.Prefix != prefix {
			t.Errorf("lifecycle prefix = %q, %v", body.Rule.Filter.Prefix, err)
		}
	}))
	defer server.Close()
	runtime := s3TestRuntime(t, server.URL)
	for _, name := range []string{ActionListObjects, ActionReplaceLifecycle} {
		prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: name, Input: map[string]any{"prefix": prefix, "expire_current_after_days": 30}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := New().ExecuteAction(context.Background(), runtime, prepared); err != nil {
			t.Fatal(err)
		}
	}
}
