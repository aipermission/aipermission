package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
)

func TestS3AcceptedArchiveComponentsSurviveActualWriter(t *testing.T) {
	objectStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected remote access") }))
	defer objectStore.Close()
	fixture := newAPITestFixture(t)
	id := createS3IdentityRuntime(t, fixture.server, objectStore.URL).TransferRuntimeID
	adapter, err := (fileTransferHandlers{fixture.server}).fileTransferAdapter(context.Background(), fixture.server.activeRuntime(), id)
	if err != nil {
		t.Fatal(err)
	}
	policy := adapter.(connectorapi.FileTransferPathPolicy)
	for _, value := range []string{"/daily/report.txt", "/nested/caf\u00e9", "/nested/cafe\u0301", "/a b/file.txt", "/" + strings.Repeat("x", 160)} {
		if err := policy.ValidateDownloadPaths([]string{value}); err != nil {
			t.Fatalf("ordinary mapping rejected: %v", err)
		}
		if mapped := relativeArchiveEntryPath(value, ""); mapped != strings.TrimPrefix(value, "/") {
			t.Fatalf("accepted %q maps to %q", value, mapped)
		}
	}
}
