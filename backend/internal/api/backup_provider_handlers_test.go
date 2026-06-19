package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestBackupProviderRoutesStoreMetadataWithoutExposingSecrets(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	catalogResponse := performJSON(handler, http.MethodGet, "/api/backup/providers/catalog", "", nil)
	if catalogResponse.Code != http.StatusOK || !strings.Contains(catalogResponse.Body.String(), "google_drive") {
		t.Fatalf("backup provider catalog failed: %d %s", catalogResponse.Code, catalogResponse.Body.String())
	}

	createResponse := performJSON(handler, http.MethodPost, "/api/backup/providers", "", map[string]any{
		"provider_type": "google_drive",
		"name":          "Personal Drive",
		"public": map[string]any{
			"folder_name": "AIPermission Backups",
		},
		"secret": map[string]any{
			"client_secret": "secret-client-secret",
		},
	})
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create backup provider failed: %d %s", createResponse.Code, createResponse.Body.String())
	}
	createBody := createResponse.Body.String()
	if !strings.Contains(createBody, `"has_secret":true`) ||
		!strings.Contains(createBody, `"has_oauth_client_secret":true`) ||
		strings.Contains(createBody, `"has_oauth_token":true`) ||
		strings.Contains(createBody, "secret-client-secret") {
		t.Fatalf("backup provider response leaked or missed secret state: %s", createBody)
	}
	created := decodeRouteResponse[backupProviderResponse](t, createResponse.Body.Bytes())

	var encrypted string
	if err := fixture.db.QueryRow(`SELECT encrypted_secret_json FROM backup_providers WHERE id = ?`, created.ID).Scan(&encrypted); err != nil {
		t.Fatalf("read encrypted backup provider secret: %v", err)
	}
	if encrypted == "" || strings.Contains(encrypted, "secret-client-secret") {
		t.Fatalf("backup provider secret was not encrypted: %q", encrypted)
	}

	listResponse := performJSON(handler, http.MethodGet, "/api/backup/providers", "", nil)
	if listResponse.Code != http.StatusOK ||
		!strings.Contains(listResponse.Body.String(), "Personal Drive") ||
		strings.Contains(listResponse.Body.String(), "secret-client-secret") {
		t.Fatalf("list backup providers failed or leaked secret: %d %s", listResponse.Code, listResponse.Body.String())
	}

	updateResponse := performJSON(handler, http.MethodPut, "/api/backup/providers/"+strconv.FormatInt(created.ID, 10), "", map[string]any{
		"name":   "Personal Drive Disabled",
		"status": "disabled",
		"public": map[string]any{
			"folder_name": "AIPermission Backups",
		},
	})
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), `"status":"disabled"`) || !strings.Contains(updateResponse.Body.String(), `"has_secret":true`) {
		t.Fatalf("update backup provider failed: %d %s", updateResponse.Code, updateResponse.Body.String())
	}
	statusOnlyResponse := performJSON(handler, http.MethodPut, "/api/backup/providers/"+strconv.FormatInt(created.ID, 10), "", map[string]any{
		"name":   "Personal Drive Disabled",
		"status": "active",
	})
	if statusOnlyResponse.Code != http.StatusOK || !strings.Contains(statusOnlyResponse.Body.String(), "AIPermission Backups") {
		t.Fatalf("status-only backup provider update should preserve public metadata: %d %s", statusOnlyResponse.Code, statusOnlyResponse.Body.String())
	}

	recordsResponse := performJSON(handler, http.MethodGet, "/api/backup/providers/"+strconv.FormatInt(created.ID, 10)+"/records", "", nil)
	if recordsResponse.Code != http.StatusOK || !strings.Contains(recordsResponse.Body.String(), `"items":[]`) {
		t.Fatalf("list backup records failed: %d %s", recordsResponse.Code, recordsResponse.Body.String())
	}
	uploadNotConnected := performJSON(handler, http.MethodPost, "/api/backup/providers/"+strconv.FormatInt(created.ID, 10)+"/upload", "", map[string]any{})
	if uploadNotConnected.Code != http.StatusConflict || !strings.Contains(uploadNotConnected.Body.String(), "access token") {
		t.Fatalf("upload without connected Google token should fail cleanly: %d %s", uploadNotConnected.Code, uploadNotConnected.Body.String())
	}
	googleStartMissingClient := performJSON(handler, http.MethodPost, "/api/backup/providers/"+strconv.FormatInt(created.ID, 10)+"/google/device/start", "", map[string]any{})
	if googleStartMissingClient.Code != http.StatusBadRequest || !strings.Contains(googleStartMissingClient.Body.String(), "client id") {
		t.Fatalf("google device flow without client id should fail cleanly: %d %s", googleStartMissingClient.Code, googleStartMissingClient.Body.String())
	}

	deleteResponse := performJSON(handler, http.MethodDelete, "/api/backup/providers/"+strconv.FormatInt(created.ID, 10), "", nil)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("archive backup provider failed: %d %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listAfterDeleteResponse := performJSON(handler, http.MethodGet, "/api/backup/providers", "", nil)
	if listAfterDeleteResponse.Code != http.StatusOK || strings.Contains(listAfterDeleteResponse.Body.String(), "Personal Drive Disabled") {
		t.Fatalf("archived backup provider should not be listed: %d %s", listAfterDeleteResponse.Code, listAfterDeleteResponse.Body.String())
	}
	recordsAfterDeleteResponse := performJSON(handler, http.MethodGet, "/api/backup/providers/"+strconv.FormatInt(created.ID, 10)+"/records", "", nil)
	if recordsAfterDeleteResponse.Code != http.StatusNotFound {
		t.Fatalf("archived backup provider records should be hidden: %d %s", recordsAfterDeleteResponse.Code, recordsAfterDeleteResponse.Body.String())
	}
}

func TestBackupProviderRoutesRequireUnlockedDatabase(t *testing.T) {
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	if response := performJSON(locked.Handler(), http.MethodGet, "/api/backup/providers/catalog", "", nil); response.Code != http.StatusLocked {
		t.Fatalf("locked backup provider catalog should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(locked.Handler(), http.MethodPost, "/api/backup/providers", "", map[string]any{"provider_type": "google_drive", "name": "Drive"}); response.Code != http.StatusLocked {
		t.Fatalf("locked backup provider create should fail, got %d %s", response.Code, response.Body.String())
	}
}

func TestBackupProviderRoutesReturnCleanValidationErrors(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	first := performJSON(handler, http.MethodPost, "/api/backup/providers", "", map[string]any{
		"provider_type": "google_drive",
		"name":          "Personal Drive",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("create backup provider failed: %d %s", first.Code, first.Body.String())
	}
	duplicate := performJSON(handler, http.MethodPost, "/api/backup/providers", "", map[string]any{
		"provider_type": "google_drive",
		"name":          "Personal Drive",
	})
	if duplicate.Code != http.StatusBadRequest || !strings.Contains(duplicate.Body.String(), "already exists") {
		t.Fatalf("duplicate backup provider should fail cleanly: %d %s", duplicate.Code, duplicate.Body.String())
	}
	unsupported := performJSON(handler, http.MethodPost, "/api/backup/providers", "", map[string]any{
		"provider_type": "dropbox",
		"name":          "Dropbox",
	})
	if unsupported.Code != http.StatusBadRequest || !strings.Contains(unsupported.Body.String(), "unsupported") {
		t.Fatalf("unsupported backup provider should fail cleanly: %d %s", unsupported.Code, unsupported.Body.String())
	}
	missing := performJSON(handler, http.MethodPut, "/api/backup/providers/9999", "", map[string]any{
		"name": "Missing",
	})
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing backup provider update should be 404: %d %s", missing.Code, missing.Body.String())
	}
	deleteMissing := performJSON(handler, http.MethodDelete, "/api/backup/providers/9999", "", nil)
	if deleteMissing.Code != http.StatusNotFound {
		t.Fatalf("missing backup provider delete should be 404: %d %s", deleteMissing.Code, deleteMissing.Body.String())
	}
}
