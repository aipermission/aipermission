package s3connector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransferPathPolicyRejectsUnsupportedMappings(t *testing.T) {
	for _, value := range []string{"/", "/folder/", "/control\tkey", "relative", "/" + strings.Repeat("x", 1025)} {
		if _, err := NormalizeTransferPath(value, false); err == nil {
			t.Fatalf("accepted unsupported file %q", value)
		}
	}
	for _, value := range []string{"/.env", "/a/../b", "/a//b", "//a", "/a\\b", "/ space", "/a.", "/" + strings.Repeat("x", 161)} {
		if err := ValidateDownloadPaths([]string{value}); err == nil {
			t.Fatalf("accepted lossy ZIP mapping %q", value)
		}
	}
	if err := ValidateDownloadPaths([]string{"/daily/a.txt", "/daily/nested/b.txt"}); err != nil {
		t.Fatal(err)
	}
	for _, key := range identityKeys {
		locator, err := NormalizeTransferPath("/"+key, false)
		if err != nil || objectKey(locator) != key {
			t.Fatalf("roundtrip %q: %q %v", key, locator, err)
		}
	}
}

func TestTransferListingShowsFolderMarkerButRejectsRecursiveTransfer(t *testing.T) {
	for _, size := range []int{0, 4} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = fmt.Fprintf(w, `<ListBucketResult><Contents><Key>folder/</Key><Size>%d</Size></Contents><Contents><Key>folder/file</Key><Size>1</Size></Contents></ListBucketResult>`, size)
			}))
			defer server.Close()
			runtime := s3TestRuntime(t, server.URL)
			entries, err := BrowseRemoteFiles(context.Background(), runtime, "/folder/")
			if err != nil || len(entries) != 2 {
				t.Fatalf("browse must retain marker and neighboring file: %#v, %v", entries, err)
			}
			var marker, file bool
			for _, entry := range entries {
				if entry.Path == "/folder/" {
					marker = entry.Type == "file" && entry.Name == "folder/" && entry.Size == int64(size)
					if _, err := NormalizeTransferPath(entry.Path, false); err == nil {
						t.Fatal("marker file operation must reject before mutation")
					}
				}
				file = file || entry.Path == "/folder/file"
			}
			if !marker || !file {
				t.Fatalf("missing exact marker or neighboring file: %#v", entries)
			}
			if _, err := ListRecursiveFiles(context.Background(), runtime, "/folder/", 100, 1000, 1000); err == nil {
				t.Fatal("recursive list silently omitted marker")
			}
		})
	}
}
