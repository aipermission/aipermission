package s3connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestDownloadObjectFitsSharedResultProjectionAtLimit(t *testing.T) {
	content := strings.Repeat("x", maxDownloadBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	result := executePreparedDownload(t, server.URL, maxDownloadBytes)
	passthrough := func(_ context.Context, value string) string { return value }
	projector, err := actionresult.NewRedactor(passthrough, passthrough, actionresult.MaxEncodedBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projector.Result(context.Background(), result); err != nil {
		t.Fatalf("project maximum download: %v", err)
	}
}

func TestDownloadObjectCatalogAdvertisesProjectionSafeLimit(t *testing.T) {
	actions, err := New().GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if action.Name != ActionDownloadObject {
			continue
		}
		for _, field := range action.InputSchema.Fields {
			if field.Name == "max_bytes" && field.Default == maxDownloadBytes {
				return
			}
		}
		t.Fatalf("download max_bytes default is not %d", maxDownloadBytes)
	}
	t.Fatal("download action not found")
}

func TestDownloadObjectRejectsOneByteAboveProjectionLimit(t *testing.T) {
	content := strings.Repeat("x", maxDownloadBytes+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	runtime := s3TestRuntime(t, server.URL)
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionDownloadObject,
		Input: map[string]any{"key": "fixture.bin", "max_bytes": maxDownloadBytes},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New().ExecuteAction(context.Background(), runtime, prepared); err == nil || !strings.Contains(err.Error(), "larger than 786432 bytes") {
		t.Fatalf("oversized download error = %v", err)
	}
}

func executePreparedDownload(t *testing.T, endpoint string, maxBytes int) connectors.ActionResult {
	t.Helper()
	runtime := s3TestRuntime(t, endpoint)
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionDownloadObject,
		Input: map[string]any{"key": "fixture.bin", "max_bytes": maxBytes},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := New().ExecuteAction(context.Background(), runtime, prepared)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
