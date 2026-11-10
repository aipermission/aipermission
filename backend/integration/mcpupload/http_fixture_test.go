package mcpupload

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/api"
	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type uploadFixture struct {
	t        *testing.T
	handler  http.Handler
	cookies  map[string]*http.Cookie
	dataPath string
}

type uploadTarget struct {
	ID       int64 `json:"id"`
	Profiles []struct {
		ID int64 `json:"id"`
	} `json:"profiles"`
	TargetRef string `json:"-"`
}

func newUploadFixture(t *testing.T) (*uploadFixture, uploadTarget) {
	t.Helper()
	registry := connectors.NewRegistry()
	adapters := connectorapi.NewRegistry()
	if err := builtin.RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	if err := builtin.RegisterAdapters(adapters); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(t.TempDir(), "test.db")
	server := api.NewLockedServer(config.Config{
		Host: "127.0.0.1", Port: "8080", DataPath: dataPath,
		GatewaySecret: "fixture-only-gateway",
	}, api.WithConnectorRegistry(registry), api.WithConnectorAdapterRegistry(adapters))
	t.Cleanup(server.Close)
	f := &uploadFixture{t: t, handler: server.Handler(), cookies: make(map[string]*http.Cookie), dataPath: dataPath}
	f.expect(http.MethodPost, "/api/unlock/setup", 200, map[string]any{
		"password": "DisposableFixturePassword123", "confirm_password": "DisposableFixturePassword123", "database_name": "Upload test",
	})
	settings := decodeFixture[struct {
		Revision string `json:"revision"`
	}](t, f.expect(http.MethodGet, "/api/settings/security", 200, nil))
	f.expect(http.MethodPut, "/api/settings/security", 200, map[string]any{
		"expected_revision": settings.Revision, "mcp_start_enabled": true, "reusable_tokens": false,
		"expose_mcp_server_metadata": false, "redaction_mode": "basic",
	})
	f.expect(http.MethodPut, "/api/settings/mcp-runtime", 200, map[string]any{"enabled": true})
	return f, f.sshTarget()
}

func (f *uploadFixture) sshTarget() uploadTarget {
	addr, key := startUploadSFTPFixture(f.t)
	keyRecord := decodeFixture[struct {
		ID int64 `json:"id"`
	}](f.t, f.expect(http.MethodPost, "/api/connectors/ssh/credentials", 201, map[string]any{"name": "test", "key_type": "ed25519"}))
	f.expect(http.MethodPost, "/api/connectors/ssh/host-keys/approve", 200, map[string]any{
		"host": addr.IP.String(), "port": addr.Port, "public_key": base64.StdEncoding.EncodeToString(key.Marshal()),
	})
	target := decodeFixture[uploadTarget](f.t, f.expect(http.MethodPost, "/api/connector-targets/with-profile", 201, map[string]any{
		"target":  map[string]any{"connector_kind": "ssh", "name": "upload-target", "config": map[string]any{"host": addr.IP.String(), "port": addr.Port}},
		"profile": map[string]any{"kind": "private_key", "label": "fixture", "public": map[string]any{"username": "fixture", "ssh_key_id": keyRecord.ID}},
	}))
	if len(target.Profiles) != 1 {
		f.t.Fatalf("unexpected target profile: %#v", target)
	}
	target.TargetRef = connectors.FormatTargetRef("ssh", target.ID, target.Profiles[0].ID)
	return target
}

func (f *uploadFixture) token(name string) tokens.CreateResponse {
	return decodeFixture[tokens.CreateResponse](f.t, f.expect(http.MethodPost, "/api/tokens", 201, map[string]any{"name": name}))
}

func (f *uploadFixture) grantUpload(target uploadTarget, tokenID int64, rule connectortargets.ActionPermissionRule) {
	url := fmt.Sprintf("/api/tokens/%d/connector-permissions", tokenID)
	current := decodeFixture[struct {
		Revision string `json:"revision"`
	}](f.t, f.expect(http.MethodGet, url, 200, nil))
	permissions := []map[string]any{{"target_id": target.ID, "profile_id": target.Profiles[0].ID, "action_name": "get_file_upload_status", "execution_rule": "always_run"}}
	if rule != "" {
		permissions = append(permissions, map[string]any{"target_id": target.ID, "profile_id": target.Profiles[0].ID, "action_name": "upload_file", "execution_rule": rule})
	}
	f.expect(http.MethodPut, url, 200, map[string]any{"expected_revision": current.Revision, "permissions": permissions})
}

func (f *uploadFixture) approve(id int64) *httptest.ResponseRecorder {
	url := fmt.Sprintf("/api/connector-action-approvals/%d", id)
	current := decodeFixture[struct {
		Hash string `json:"approval_context_hash"`
	}](f.t, f.expect(http.MethodGet, url, 200, nil))
	return f.request(http.MethodPost, url+"/run", "", map[string]any{"approval_context_hash": current.Hash})
}

func decodeFixture[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode fixture response: %v", err)
	}
	return value
}

func (f *uploadFixture) expect(method, url string, status int, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	response := f.request(method, url, "", body)
	if response.Code != status {
		f.t.Fatalf("%s %s: %d %s", method, url, response.Code, response.Body)
	}
	return response
}

func (f *uploadFixture) request(method, url, token string, body any) *httptest.ResponseRecorder {
	request := fixtureRequest(method, url, token, body)
	for _, cookie := range f.cookies {
		request.AddCookie(cookie)
		switch cookie.Name {
		case "aipermission_csrf":
			request.Header.Set("X-AIPermission-CSRF", cookie.Value)
		case "aipermission_workspace":
			request.Header.Set("X-AIPermission-Workspace", cookie.Value)
		}
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		f.cookies[cookie.Name] = cookie
	}
	return response
}

func performJSON(handler http.Handler, method, url, token string, body any) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, fixtureRequest(method, url, token, body))
	return response
}

func fixtureRequest(method, url, token string, body any) *http.Request {
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(method, "http://localhost"+url, bytes.NewReader(encoded))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}
