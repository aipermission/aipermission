package s3connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestTruncatedS3ListingsRequireForwardProgress(t *testing.T) {
	for _, next := range []string{"", "current"} {
		t.Run("next="+next, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("versions") {
					_, _ = fmt.Fprintf(w, `<ListVersionsResult><IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextVersionIdMarker>version</NextVersionIdMarker></ListVersionsResult>`, next)
				} else {
					_, _ = fmt.Fprintf(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>%s</NextContinuationToken></ListBucketResult>`, next)
				}
			}))
			defer server.Close()
			client, err := newS3Client(t.Context(), s3TestRuntime(t, server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ListObjects(t.Context(), "", "current", 10, false); err == nil {
				t.Fatal("accepted missing or repeated object cursor")
			}
			if _, err := client.ListObjectVersions(t.Context(), "key", s3VersionCursor{KeyMarker: "current", VersionIDMarker: "version"}, 10); err == nil {
				t.Fatal("accepted missing or repeated version cursor")
			}
		})
	}
}

func TestS3MultiPageReadsRejectCursorCycles(t *testing.T) {
	for _, operation := range []string{"search", "versions", "recursive"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls >= 10 {
					cancel()
				}
				next := "first"
				if calls%2 == 0 {
					next = "second"
				}
				if r.URL.Query().Has("versions") {
					_, _ = fmt.Fprintf(w, `<ListVersionsResult><IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker></ListVersionsResult>`, next)
				} else {
					_, _ = fmt.Fprintf(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>%s</NextContinuationToken></ListBucketResult>`, next)
				}
			}))
			defer server.Close()
			runtime := s3TestRuntime(t, server.URL)
			client, err := newS3Client(t.Context(), runtime)
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "search":
				_, err = executeSearchObjects(ctx, client, "", "needle", "", 10)
			case "versions":
				_, err = executeListObjectVersions(ctx, client, map[string]any{"key": "key"})
			case "recursive":
				_, err = ListRecursiveFiles(ctx, runtime, "/", 100, 1024, 4096)
			}
			if err == nil || calls != 3 {
				t.Fatalf("cycle was not rejected promptly: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestS3RecursiveListingBoundsAdvancingEmptyPages(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if complete && calls == maxS3RecursivePages {
					_, _ = fmt.Fprint(w, `<ListBucketResult><Contents><Key>last.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
					return
				}
				_, _ = fmt.Fprintf(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>page-%d</NextContinuationToken></ListBucketResult>`, calls)
			}))
			defer server.Close()
			entries, err := ListRecursiveFiles(t.Context(), s3TestRuntime(t, server.URL), "/", 100, 1024, 4096)
			if calls != maxS3RecursivePages {
				t.Fatalf("calls=%d", calls)
			}
			if complete {
				if err != nil || len(entries) != 1 || entries[0].Path != "/last.txt" {
					t.Fatalf("entries=%v err=%v", entries, err)
				}
			} else if !errors.Is(err, ErrTransferLimit) || entries != nil {
				t.Fatalf("partial listing accepted: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestS3ProgressUsesExactOpaqueMarkers(t *testing.T) {
	seen := make(map[string]struct{})
	for _, step := range [][2]string{{"", " "}, {" ", "  "}, {"  ", "+%2F="}} {
		if err := requireListingProgress(step[0], step[1], false, seen); err != nil {
			t.Fatal(err)
		}
	}
	if err := requireListingProgress("+%2F=", " ", false, seen); err == nil {
		t.Fatal("accepted whitespace cursor cycle")
	}
	current := s3VersionCursor{KeyMarker: "same key", VersionIDMarker: "first"}
	next := s3VersionCursor{KeyMarker: "same key", VersionIDMarker: "second"}
	if err := requireListingProgress(current, next, false, nil); err != nil {
		t.Fatal("rejected same-key version advancement:", err)
	}
}

func TestS3ObjectCursorRetainsOpaqueWhitespaceThroughPreparationAndBrowse(t *testing.T) {
	token := " +opaque%2F= "
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("continuation-token") != token {
			t.Errorf("cursor changed: %q", r.URL.Query().Get("continuation-token"))
		}
		_, _ = fmt.Fprint(w, `<ListBucketResult/>`)
	}))
	defer server.Close()
	runtime := s3TestRuntime(t, server.URL)
	prepared, err := New().PrepareAction(t.Context(), connectors.ActionRequest{ActionName: ActionListObjects, Target: runtime.Target, Input: map[string]any{"cursor": token}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = New().ExecuteAction(t.Context(), runtime, prepared); err != nil {
		t.Fatal(err)
	}
	if _, err = BrowseRemoteFilesPage(t.Context(), runtime, "/", token); err != nil {
		t.Fatal(err)
	}
}
