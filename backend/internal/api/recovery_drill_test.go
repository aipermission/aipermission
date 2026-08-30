package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
)

func TestRecoveryDrillEncryptedBackupWrongPasswordAndGatewaySecret(t *testing.T) {
	const (
		databasePassword      = "RecoveryPassword123"
		originalGatewaySecret = "recovery-original-gateway-secret-0123456789"
		restartFallbackSecret = "recovery-restart-fallback-secret-0123456789"
	)
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	cfg := config.Config{
		Host:           "127.0.0.1",
		Port:           "8080",
		DataPath:       dataPath,
		GatewaySecret:  originalGatewaySecret,
		AllowedOrigins: []string{"http://localhost:3001"},
	}
	server := NewLockedServer(cfg)
	serverClosed := false
	t.Cleanup(func() {
		if !serverClosed {
			server.Close()
		}
	})
	handler := server.Handler()

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        databasePassword,
		ConfirmPassword: databasePassword,
		DatabaseName:    "Recovery Source",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup recovery source: %d %s", setup.Code, setup.Body.String())
	}
	if _, err := server.activeRuntime().database.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('recovery_drill_marker', 'preserved', datetime('now'))`); err != nil {
		t.Fatalf("insert recovery marker: %v", err)
	}
	snapshot, err := createDatabaseSnapshot(server.activeRuntime())
	if err != nil {
		t.Fatalf("create encrypted recovery snapshot: %v", err)
	}
	defer os.Remove(snapshot.Path)

	if response := performJSON(handler, http.MethodPost, "/api/lock", "", map[string]string{"scope": "all"}); response.Code != http.StatusOK {
		t.Fatalf("lock source database: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{
		DatabaseID: "recovery-source",
		Password:   "WrongRecoveryPassword123",
	}); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password should be rejected: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{
		DatabaseID: "recovery-source",
		Password:   databasePassword,
	}); response.Code != http.StatusOK {
		t.Fatalf("unlock source with correct password: %d %s", response.Code, response.Body.String())
	}

	importResponse := performRecoveryImport(t, handler, snapshot.Path, "Recovered Copy", databasePassword)
	if importResponse.Code != http.StatusOK || !strings.Contains(importResponse.Body.String(), `"database_id":"recovered-copy"`) {
		t.Fatalf("restore encrypted snapshot: %d %s", importResponse.Code, importResponse.Body.String())
	}
	assertRecoveryMarker(t, server)
	if server.config.GatewaySecret != originalGatewaySecret {
		t.Fatalf("restored runtime gateway secret = %q", server.config.GatewaySecret)
	}
	server.Close()
	serverClosed = true

	restartedConfig := cfg
	restartedConfig.GatewaySecret = restartFallbackSecret
	restarted := NewLockedServer(restartedConfig)
	defer restarted.Close()
	unlock := performJSON(restarted.Handler(), http.MethodPost, "/api/unlock", "", unlockRequest{
		DatabaseID: "recovered-copy",
		Password:   databasePassword,
	})
	if unlock.Code != http.StatusOK {
		t.Fatalf("unlock restored database after restart: %d %s", unlock.Code, unlock.Body.String())
	}
	assertRecoveryMarker(t, restarted)
	if restarted.config.GatewaySecret != originalGatewaySecret {
		t.Fatalf("restart used fallback secret %q instead of stored secret", restarted.config.GatewaySecret)
	}
}

func performRecoveryImport(t *testing.T, handler http.Handler, snapshotPath, databaseName, password string) *httptest.ResponseRecorder {
	t.Helper()
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read recovery snapshot: %v", err)
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("database_name", databaseName); err != nil {
		t.Fatalf("write recovery database name: %v", err)
	}
	if err := writer.WriteField("database_password", password); err != nil {
		t.Fatalf("write recovery database password: %v", err)
	}
	part, err := writer.CreateFormFile("sqlite", "recovery.aipdb")
	if err != nil {
		t.Fatalf("create recovery upload: %v", err)
	}
	if _, err := part.Write(snapshot); err != nil {
		t.Fatalf("write recovery upload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close recovery upload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", body)
	request.Host = "localhost:8080"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if cookie := currentTestUICookie(); cookie != nil {
		request.AddCookie(cookie)
	}
	request.AddCookie(&http.Cookie{Name: uiCSRFCookieName, Value: testUICSRFToken})
	request.Header.Set(uiCSRFHeaderName, testUICSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	recordTestUICookies(response.Result().Cookies())
	return response
}

func assertRecoveryMarker(t *testing.T, server *Server) {
	t.Helper()
	var marker string
	if err := server.activeRuntime().database.QueryRow(`SELECT value FROM settings WHERE key = 'recovery_drill_marker'`).Scan(&marker); err != nil {
		t.Fatalf("read recovery marker: %v", err)
	}
	if marker != "preserved" {
		t.Fatalf("recovery marker = %q", marker)
	}
}
