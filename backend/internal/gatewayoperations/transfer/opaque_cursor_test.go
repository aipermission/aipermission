package gatewaytransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

type opaqueCursorAdapter struct {
	rejectingTransferAdapter
	token string
	calls int
}

func (a *opaqueCursorAdapter) BrowseRemoteFilesPage(_ context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, _, cursor string) (connectorapi.RemoteFilePage, error) {
	a.calls++
	if a.calls == 1 && cursor == "" {
		return connectorapi.RemoteFilePage{NextCursor: a.token, HasMore: true}, nil
	}
	if a.calls != 2 || cursor != a.token {
		return connectorapi.RemoteFilePage{}, fmt.Errorf("cursor changed: %q", cursor)
	}
	return connectorapi.RemoteFilePage{}, nil
}

func TestBrowseRemoteFilesPreservesOpaquePaginationCursor(t *testing.T) {
	for _, token := range []string{" +opaque%2F= ", " "} {
		t.Run(token, func(t *testing.T) {
			adapter := &opaqueCursorAdapter{token: token}
			fixture := newTransferTestFixtureWithAdapter(t, adapter)
			fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) { return fixture.runtime, true }
			cursor := ""
			for page := 0; page < 2; page++ {
				body, err := json.Marshal(browseRemoteFilesRequest{RuntimeID: fixture.runtimeID, Path: "/", Cursor: cursor})
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				fixture.handlers.BrowseRemoteFiles(response, request)
				if response.Code != http.StatusOK {
					t.Fatalf("page %d: status=%d body=%s", page, response.Code, response.Body.String())
				}
				var result browseRemoteFilesResponse
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				cursor = result.NextCursor
				if page == 0 && (cursor != token || !result.HasMore) {
					t.Fatalf("first page cursor = %q, has_more=%v", cursor, result.HasMore)
				}
				if page == 1 && result.HasMore {
					t.Fatal("terminal page reports more results")
				}
			}
			if adapter.calls != 2 {
				t.Fatalf("dispatch count = %d, want 2", adapter.calls)
			}
		})
	}
}

func TestBrowseRemoteFilesRejectsOpaqueCursorForNonpaginatedAdapter(t *testing.T) {
	fixture := newTransferTestFixture(t)
	fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) { return fixture.runtime, true }
	body, err := json.Marshal(browseRemoteFilesRequest{RuntimeID: fixture.runtimeID, Path: "/", Cursor: " "})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	fixture.handlers.BrowseRemoteFiles(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["error"] != "this connector does not support paginated file browsing" {
		t.Fatalf("unexpected rejection: %s, %v", response.Body.String(), err)
	}
}
