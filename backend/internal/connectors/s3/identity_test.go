package s3connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var identityKeys = []string{"invoice", "invoice ", " invoice", "/invoice", "a//b", "a/../b", "a/./b", "caf\u00e9", "cafe\u0301", " ", "a%2Fb", "a+b?#", "a\\b"}

func TestExactObjectIdentityThroughPreparedActions(t *testing.T) {
	for _, key := range append(append([]string{}, identityKeys...), "folder/", "a\tb") {
		t.Run(key, func(t *testing.T) {
			var targets []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targets = append(targets, r.Method+" "+r.RequestURI)
				w.Header().Set("Content-Length", "0")
			}))
			defer server.Close()
			runtime := s3TestRuntime(t, server.URL)
			for _, action := range []string{ActionGetObjectMetadata, ActionDownloadObject, ActionDeleteObject} {
				prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: action, Input: map[string]any{"key": key}})
				if err != nil {
					t.Fatal(err)
				}
				if prepared.Payload["key"] != key {
					t.Fatalf("prepared key = %q, want %q", prepared.Payload["key"], key)
				}
				if _, err := New().ExecuteAction(context.Background(), runtime, prepared); err != nil {
					t.Fatal(err)
				}
			}
			for i, method := range []string{"HEAD", "GET", "DELETE"} {
				want := method + " /test-bucket/" + awsPathEscape(key)
				if targets[i] != want {
					t.Fatalf("target = %q, want %q", targets[i], want)
				}
			}
		})
	}
}

func TestExactTransferDownloadIdentity(t *testing.T) {
	for _, key := range identityKeys {
		t.Run(key, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/test-bucket/"+key {
					t.Errorf("requested %q, want key %q", r.URL.Path, key)
				}
				w.Header().Set("Content-Length", "4")
				if r.Method != http.MethodHead {
					_, _ = w.Write([]byte("data"))
				}
			}))
			defer server.Close()
			local := filepath.Join(t.TempDir(), "download")
			_, err := DownloadFile(context.Background(), s3TestRuntime(t, server.URL), virtualObjectPath(key), local, TransferOptions{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(local)
			if err != nil || string(data) != "data" {
				t.Fatalf("download = %q, %v", data, err)
			}
		})
	}
}
