package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsUntrustedRequestShapes(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		limit       int64
		status      int
	}{
		{name: "wrong content type", contentType: "text/plain", body: `{}`, status: http.StatusBadRequest},
		{name: "unknown field", contentType: "application/json", body: `{"extra":true}`, status: http.StatusBadRequest},
		{name: "trailing value", contentType: "application/json", body: `{} {}`, status: http.StatusBadRequest},
		{name: "too large", contentType: "application/json", body: `{"name":"long"}`, limit: 4, status: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			var target struct {
				Name string `json:"name"`
			}
			if DecodeJSON(response, request, &target, test.limit) {
				t.Fatal("expected decode failure")
			}
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestDecodeJSONAcceptsOneStrictObject(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	var target struct {
		Name string `json:"name"`
	}
	if !DecodeJSON(response, request, &target, 0) || target.Name != "ok" {
		t.Fatalf("decoded target = %#v, response = %s", target, response.Body.String())
	}
}

func TestWorkspaceBoundReadCatalog(t *testing.T) {
	for _, path := range []string{
		"/api/backup/download",
		"/api/settings/diagnostics",
		"/api/backup/providers/3/records/9/download",
		"/api/file-transfers/4/download",
		"/api/file-transfer-batches/4/download",
		"/api/connector-targets/1/profiles/2/backup",
		"/api/console/sessions/4/attach",
		"/api/settings/maintenance-console/attach",
	} {
		if !IsWorkspaceBoundRead(http.MethodGet, path) {
			t.Errorf("download route %s is not workspace-bound", path)
		}
	}
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/api/backup/download"},
		{http.MethodGet, "/api/status"},
		{http.MethodGet, "/api/backup/providers/3/records"},
	} {
		if IsWorkspaceBoundRead(request.method, request.path) {
			t.Errorf("ordinary route %s %s is workspace-bound", request.method, request.path)
		}
	}
}

func TestWorkspaceSocketRouteCatalog(t *testing.T) {
	for _, path := range []string{
		"/api/settings/maintenance-console/attach",
		"/api/console/sessions/12/attach",
	} {
		if !IsWorkspaceSocketRoute(path) {
			t.Fatalf("workspace socket route %q was not recognized", path)
		}
	}
	for _, path := range []string{
		"/api/settings/maintenance-console/status",
		"/api/console/sessions/12",
		"/api/console/sessions/12/attach/extra",
	} {
		if IsWorkspaceSocketRoute(path) {
			t.Fatalf("ordinary route %q was classified as a workspace socket", path)
		}
	}
}
