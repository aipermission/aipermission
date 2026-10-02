package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestFileTransferControlRoutesUseTransferOwnerAndRequireUI(t *testing.T) {
	fixture := newAPITestFixture(t)
	workspace := currentTestUIWorkspaceBinding()
	token, err := fixture.tokens.Create(t.Context(), tokens.CreateRequest{Name: "transfer-controls"})
	if err != nil {
		t.Fatal(err)
	}
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	t.Cleanup(locked.Close)
	for _, testCase := range []struct {
		path    string
		status  int
		message string
	}{
		{"/api/file-transfers/37/cancel", http.StatusNotFound, "file transfer not found"},
		{"/api/file-transfer-batches/37/cancel", http.StatusNotFound, "file transfer batch not found"},
		{"/api/file-transfer-batches/37/pause", http.StatusConflict, "file transfer batch is not active"},
		{"/api/file-transfer-batches/37/resume", http.StatusConflict, "file transfer batch is not active"},
	} {
		t.Run(testCase.path, func(t *testing.T) {
			response := performJSONForWorkspace(fixture.server.Handler(), http.MethodPost, testCase.path, nil, workspace)
			if response.Code != testCase.status || !strings.Contains(response.Body.String(), testCase.message) {
				t.Fatalf("transfer owner response=%d body=%s", response.Code, response.Body.String())
			}
			response = performJSON(fixture.server.Handler(), http.MethodPost, testCase.path, token.TokenValue, nil)
			if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "ui session required") {
				t.Fatalf("API token must not authorize UI control: %d %s", response.Code, response.Body.String())
			}
			response = performJSON(locked.Handler(), http.MethodPost, testCase.path, "", nil)
			if response.Code != http.StatusLocked || !strings.Contains(response.Body.String(), "database is locked") {
				t.Fatalf("locked UI control response=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
