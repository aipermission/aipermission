package connectormanagement

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const profileBackupTestKind = "profile_backup_test"

type profileBackupTestConnector struct {
	managementTestConnector
	restored string
}

func (*profileBackupTestConnector) Kind() string { return profileBackupTestKind }

func (*profileBackupTestConnector) Backup(context.Context, connectors.RuntimeContext, connectors.BackupRequest) (connectors.BackupArtifact, error) {
	return connectors.BackupArtifact{Filename: "database.sql", ContentType: "application/sql", Data: []byte("select 1;\n")}, nil
}

func (c *profileBackupTestConnector) Restore(_ context.Context, _ connectors.RuntimeContext, request connectors.RestoreRequest) (connectors.ActionResult, error) {
	content, err := io.ReadAll(request.Content)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	c.restored = string(content)
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"restored": true}}, nil
}

func TestProfileBackupHTTPHandlerOwnsDownloadAndConfirmedRestore(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	connector := &profileBackupTestConnector{}
	registry := connectors.NewRegistry()
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(fixture.database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: profileBackupTestKind, Name: "Production database", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: profileBackupTestKind, Kind: "operator", Label: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := make([]string, 0, 2)
	handler := NewProfileBackupHTTPHandler(func(http.ResponseWriter) (ProfileBackupScope, bool) {
		return ProfileBackupScope{
			Database: fixture.database, Registry: registry,
			Runtime: CredentialRuntimePorts{
				DecryptSecret: func(context.Context, int64, string) (map[string]any, error) { return map[string]any{}, nil },
				RuntimeContext: func(target connectortargets.Target, profile connectortargets.CredentialProfile, _ map[string]any, _ CredentialBoundary) connectors.RuntimeContext {
					return connectors.RuntimeContext{
						Target:  connectors.TargetView{ID: target.ID, ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config},
						Profile: connectortargets.CredentialProfileView(profile),
					}
				},
				RedactResult: func(_ context.Context, result connectors.ActionResult, _ CredentialBoundary) (connectors.ActionResult, error) {
					return result, nil
				},
				RedactText: func(_ context.Context, value string) string { return value },
			},
			Observe: func(_ context.Context, action string, _ map[string]any) { observed = append(observed, action) },
		}, true
	})
	mux := http.NewServeMux()
	pattern := "/targets/{id}/profiles/{profile_id}"
	mux.HandleFunc("GET "+pattern+"/backup", handler.Download)
	mux.HandleFunc("POST "+pattern+"/restore", handler.Restore)
	basePath := "/targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(profile.ID, 10)

	download := httptest.NewRecorder()
	mux.ServeHTTP(download, httptest.NewRequest(http.MethodGet, basePath+"/backup", nil))
	if download.Code != http.StatusOK || download.Body.String() != "select 1;\n" ||
		!strings.Contains(download.Header().Get("Content-Disposition"), "database.sql") {
		t.Fatalf("download = %d headers=%v body=%q", download.Code, download.Header(), download.Body.String())
	}

	wrongBody, wrongType := profileRestoreBody(t, "Wrong target", "backup.sql", "restore payload")
	wrong := httptest.NewRecorder()
	wrongRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", wrongBody)
	wrongRequest.Header.Set("Content-Type", wrongType)
	mux.ServeHTTP(wrong, wrongRequest)
	if wrong.Code != http.StatusBadRequest || connector.restored != "" {
		t.Fatalf("unconfirmed restore = %d %s restored=%q", wrong.Code, wrong.Body.String(), connector.restored)
	}

	body, contentType := profileRestoreBody(t, target.Name, "backup.sql", "restore payload")
	restore := httptest.NewRecorder()
	restoreRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	restoreRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(restore, restoreRequest)
	if restore.Code != http.StatusOK || connector.restored != "restore payload" || len(observed) != 2 {
		t.Fatalf("restore = %d %s restored=%q observed=%v", restore.Code, restore.Body.String(), connector.restored, observed)
	}
}

func profileRestoreBody(t *testing.T, confirmation, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("confirm_target", confirmation); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("dump", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}
