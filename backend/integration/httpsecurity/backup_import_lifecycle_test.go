package httpsecurity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/api"
	apihttp "github.com/aipermission/aipermission/backend/internal/api/httptransport"
	"github.com/aipermission/aipermission/backend/internal/config"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

const importLifecyclePassword = "ImportLifecycle123"

func importLifecycleSource(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.aipdb")
	database, err := dbpkg.OpenEncrypted(path, importLifecyclePassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

type importAdmissionBody struct {
	io.ReadCloser
	started chan struct{}
	once    sync.Once
}

func (body *importAdmissionBody) Read(data []byte) (int, error) {
	body.once.Do(func() { close(body.started) })
	return body.ReadCloser.Read(data)
}

func pausedDatabaseImport(t *testing.T, client *importHTTPClient, content []byte) (func(), func() *httptest.ResponseRecorder) {
	t.Helper()
	var encoded bytes.Buffer
	form := multipart.NewWriter(&encoded)
	if err := form.WriteField("database_name", "Import Copy"); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("database_password", importLifecyclePassword); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("sqlite", "source.aipdb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	body := &importAdmissionBody{ReadCloser: reader, started: make(chan struct{})}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(func() { cancel(); _ = reader.Close(); _ = writer.Close() })
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", body).WithContext(ctx)
	request.Host, request.RemoteAddr = "localhost:8080", "127.0.0.1:12345"
	request.Header.Set("Content-Type", form.FormDataContentType())
	client.authorize(request)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		client.handler.ServeHTTP(response, request)
		done <- response
	}()
	select {
	case <-body.started:
	case response := <-done:
		t.Fatalf("import rejected before body admission: %d %s", response.Code, response.Body.String())
	case <-ctx.Done():
		t.Fatal("import did not reach body admission")
	}
	return func() {
			go func() { _, _ = io.Copy(writer, &encoded); _ = writer.Close() }()
		}, func() *httptest.ResponseRecorder {
			select {
			case response := <-done:
				return response
			case <-ctx.Done():
				t.Fatal("import did not complete")
				return nil
			}
		}
}

func TestImportUploadDoesNotBlockLifecycleAndRevalidatesBeforePublish(t *testing.T) {
	source := importLifecycleSource(t)
	for _, scenario := range []struct {
		name     string
		unlocked bool
		change   string
		want     int
	}{
		{"locked recovery", false, "", http.StatusOK},
		{"unlocked import", true, "", http.StatusOK},
		{"locked to unlocked", false, "setup", http.StatusConflict},
		{"unlocked to locked", true, "lock", http.StatusConflict},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dataPath := filepath.Join(t.TempDir(), "aipermission.db")
			server := api.NewLockedServer(config.Config{Host: "127.0.0.1", Port: "8080", DataPath: dataPath, GatewaySecret: "gateway-secret"})
			defer server.Close()
			client := &importHTTPClient{handler: server.Handler(), cookies: make(map[string]*http.Cookie)}
			if scenario.unlocked {
				setupImportLifecycleWorkspace(t, client)
			}
			resume, await := pausedDatabaseImport(t, client, source)
			for _, path := range []string{"/health", "/api/status"} {
				response := client.json(t, http.MethodGet, path, nil)
				if response.Code != http.StatusOK {
					t.Fatalf("%s blocked during upload: %d %s", path, response.Code, response.Body.String())
				}
			}
			switch scenario.change {
			case "setup":
				setupImportLifecycleWorkspace(t, client)
			case "lock":
				response := client.json(t, http.MethodPost, "/api/lock", map[string]string{"scope": "all"})
				if response.Code != http.StatusOK {
					t.Fatalf("lock blocked during upload: %d %s", response.Code, response.Body.String())
				}
			}
			resume()
			response := await()
			if response.Code != scenario.want {
				t.Fatalf("import status=%d want=%d body=%s", response.Code, scenario.want, response.Body.String())
			}
			if scenario.want != http.StatusOK {
				if _, err := os.Stat(filepath.Join(filepath.Dir(dataPath), "databases", "import-copy.db")); !os.IsNotExist(err) {
					t.Fatalf("rejected import published file: %v", err)
				}
			}
		})
	}
}

func setupImportLifecycleWorkspace(t *testing.T, client *importHTTPClient) {
	t.Helper()
	response := client.json(t, http.MethodPost, "/api/unlock/setup", map[string]string{
		"password": "WorkspacePassword123", "confirm_password": "WorkspacePassword123",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("workspace setup failed during import: %d %s", response.Code, response.Body.String())
	}
}

type importHTTPClient struct {
	handler   http.Handler
	cookies   map[string]*http.Cookie
	workspace string
}

func (client *importHTTPClient) authorize(request *http.Request) {
	for _, cookie := range client.cookies {
		request.AddCookie(cookie)
	}
	if cookie := client.cookies[uisession.CSRFCookieBase]; cookie != nil {
		request.Header.Set(uisession.CSRFHeaderName, cookie.Value)
	}
	request.Header.Set(apihttp.WorkspaceHeaderName, client.workspace)
}

func (client *importHTTPClient) json(t *testing.T, method, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Host, request.RemoteAddr = "localhost:8080", "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	client.authorize(request)
	response := httptest.NewRecorder()
	client.handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		client.cookies[cookie.Name] = cookie
	}
	if workspace := response.Header().Get(apihttp.WorkspaceHeaderName); workspace != "" {
		client.workspace = workspace
	}
	return response
}
