package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/api"
	apihttp "github.com/aipermission/aipermission/backend/internal/api/httptransport"
	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

type managedMetadataHTTPFixture struct {
	handler http.Handler
	db      *sql.DB
}

func newManagedMetadataHTTPFixture(t *testing.T) managedMetadataHTTPFixture {
	t.Helper()
	const password = "MetadataFixturePassword123"
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	registry := connectors.NewRegistry()
	if err := registry.Register(postgresconnector.New()); err != nil {
		t.Fatal(err)
	}
	server := api.NewLockedServer(config.Config{
		Host: "127.0.0.1", Port: "8080", DataPath: dataPath, GatewaySecret: "metadata-test-only",
		AllowedOrigins: []string{"http://localhost:3001"},
	}, api.WithConnectorRegistry(registry))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.CloseContext(ctx); err != nil {
			t.Errorf("close fixture server: %v", err)
		}
	})
	setup := performManagedJSON(t, server.Handler(), http.MethodPost, "/api/unlock/setup", map[string]any{
		"password": password, "confirm_password": password,
		"database_id": "default", "database_name": "Default",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup fixture: %d %s", setup.Code, setup.Body.String())
	}
	cookies := setup.Result().Cookies()
	workspace := setup.Header().Get(apihttp.WorkspaceHeaderName)
	var csrf string
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie.Name, uisession.CSRFCookieBase) {
			csrf = cookie.Value
		}
	}
	if csrf == "" || workspace == "" {
		t.Fatal("setup did not return UI authorization and workspace binding")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		r.Header.Set(uisession.CSRFHeaderName, csrf)
		r.Header.Set(apihttp.WorkspaceHeaderName, workspace)
		server.Handler().ServeHTTP(w, r)
	})
	path, err := databasecatalog.DatabasePath(dataPath, "default")
	if err != nil {
		t.Fatal(err)
	}
	database, err := appdb.OpenEncrypted(path, password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close fixture store: %v", err)
		}
	})
	return managedMetadataHTTPFixture{handler: handler, db: database}
}

func performManagedJSON(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Host = "localhost:8080"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:3001")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeManagedResponse[T any](t *testing.T, body []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
