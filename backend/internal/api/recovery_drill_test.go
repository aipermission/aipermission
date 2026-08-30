package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/config"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
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

func TestRecoveryDrillSelfHostedBackupDownloadAndRestart(t *testing.T) {
	const (
		databasePassword      = "RemoteRecoveryPassword123"
		originalGatewaySecret = "remote-recovery-original-secret-0123456789"
		fallbackGatewaySecret = "remote-recovery-fallback-secret-0123456789"
	)
	remote := newFakeBackupService(t)
	sourcePath := filepath.Join(t.TempDir(), "remote-recovery.aipdb")
	sourceDatabase, err := dbpkg.OpenEncrypted(sourcePath, databasePassword)
	if err != nil {
		t.Fatalf("create remote recovery source: %v", err)
	}
	if _, err := sourceDatabase.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES
			('gateway_secret', ?, datetime('now')),
			('recovery_drill_marker', 'preserved', datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		originalGatewaySecret,
	); err != nil {
		t.Fatalf("seed remote recovery source: %v", err)
	}
	if err := sourceDatabase.Close(); err != nil {
		t.Fatalf("close remote recovery source: %v", err)
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read remote recovery source: %v", err)
	}
	digest := sha256.Sum256(data)
	remote.mu.Lock()
	remote.data = data
	remote.item = backups.ServiceBackup{
		ID:                   "bkp_recovery_drill",
		StreamID:             "recovery-drill-stream",
		DatabaseName:         "Remote Recovery Source",
		SourceInstallationID: "recovery-drill-installation",
		Filename:             "remote-recovery.aipdb",
		SizeBytes:            int64(len(data)),
		SHA256:               hex.EncodeToString(digest[:]),
		CreatedAt:            time.Now().UTC().Format(time.RFC3339Nano),
	}
	remote.mu.Unlock()

	dataPath := filepath.Join(t.TempDir(), "aipermission.aipdb")
	cfg := config.Config{
		Host:           "127.0.0.1",
		Port:           "8080",
		DataPath:       dataPath,
		GatewaySecret:  fallbackGatewaySecret,
		AllowedOrigins: []string{"http://localhost:3001"},
	}
	server := NewLockedServer(cfg)
	serverClosed := false
	t.Cleanup(func() {
		if !serverClosed {
			server.Close()
		}
	})
	restore := performJSON(server.Handler(), http.MethodPost, "/api/backup/remote/restore", "", transientBackupRestoreRequest{
		BaseURL:          remote.server.URL,
		Token:            backupAPITestToken,
		StreamID:         remote.item.StreamID,
		BackupID:         remote.item.ID,
		DatabaseName:     "Remote Recovered",
		DatabasePassword: databasePassword,
	})
	if restore.Code != http.StatusOK || !strings.Contains(restore.Body.String(), `"database_id":"remote-recovered"`) {
		t.Fatalf("restore self-hosted recovery backup: %d %s", restore.Code, restore.Body.String())
	}
	assertRecoveryMarker(t, server)
	if server.config.GatewaySecret != originalGatewaySecret {
		t.Fatalf("remote restore used fallback gateway secret %q", server.config.GatewaySecret)
	}
	server.Close()
	serverClosed = true

	restarted := NewLockedServer(cfg)
	defer restarted.Close()
	unlock := performJSON(restarted.Handler(), http.MethodPost, "/api/unlock", "", unlockRequest{
		DatabaseID: "remote-recovered",
		Password:   databasePassword,
	})
	if unlock.Code != http.StatusOK {
		t.Fatalf("unlock remote recovery after restart: %d %s", unlock.Code, unlock.Body.String())
	}
	assertRecoveryMarker(t, restarted)
	if restarted.config.GatewaySecret != originalGatewaySecret {
		t.Fatalf("remote recovery restart used fallback gateway secret %q", restarted.config.GatewaySecret)
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
