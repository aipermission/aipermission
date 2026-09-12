package backups

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type backupHTTPServiceFixture struct {
	server  *httptest.Server
	payload []byte
	backup  ServiceBackup
}

func newBackupHTTPServiceFixture(t *testing.T) *backupHTTPServiceFixture {
	t.Helper()
	payload := []byte("encrypted-http-backup")
	digest := sha256.Sum256(payload)
	fixture := &backupHTTPServiceFixture{payload: payload}
	fixture.backup = ServiceBackup{
		ID: "bkp_http", StreamID: "workspace-test", DatabaseName: "Test Database",
		SourceInstallationID: "install-http", Filename: "test-database.aipdb",
		SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]),
		CreatedAt: "2026-09-12T10:00:00Z",
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testOldServiceToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/info":
			_ = json.NewEncoder(w).Encode(ServiceInfo{
				Service: "aipermission-backup", Version: "fixture", ProtocolVersion: ServiceProtocol,
				Capabilities:   append([]string(nil), requiredServiceCapabilities...),
				MaxUploadBytes: MaxDatabaseTransferBytes, StorageSchema: 1,
			})
		case r.URL.Path == "/v1/streams/workspace-test/backups" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(servicePage[ServiceBackup]{Items: []ServiceBackup{fixture.backup}})
		case r.URL.Path == "/v1/streams/workspace-test/backups/bkp_http" && r.Method == http.MethodGet:
			w.Header().Set("X-AIPermission-Backup-ID", fixture.backup.ID)
			w.Header().Set("X-AIPermission-SHA256", fixture.backup.SHA256)
			w.Header().Set("Content-Disposition", "attachment; filename=\""+fixture.backup.Filename+"\"")
			_, _ = w.Write(fixture.payload)
		case r.URL.Path == "/v1/storage" && r.Method == http.MethodGet:
			remaining := int64(3072)
			_ = json.NewEncoder(w).Encode(ServiceStorageUsage{
				UsedBytes: 1024, QuotaEnabled: true, QuotaBytes: 4096,
				RemainingBytes: &remaining, BackupCount: 1, StreamCount: 1,
			})
		case r.URL.Path == "/v1/streams/workspace-test/retention" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(ServiceRetentionPolicy{
				StreamID: "workspace-test", Enabled: true, KeepLatest: 10,
			})
		case r.URL.Path == "/v1/streams/workspace-test/retention/preview" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(ServiceRetentionPreview{
				StreamID: "workspace-test", KeepLatest: 5, RetainCount: 1,
			})
		case r.URL.Path == "/v1/streams/workspace-test/retention" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(ServiceRetentionUpdate{
				Policy: ServiceRetentionPolicy{
					StreamID: "workspace-test", Enabled: true, KeepLatest: 5,
				},
				Preview: ServiceRetentionPreview{
					StreamID: "workspace-test", KeepLatest: 5, RetainCount: 1,
				},
			})
		case r.URL.Path == "/v1/streams/workspace-test/prune" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(ServicePruneResult{
				StreamID: "workspace-test", KeepLatest: 5,
			})
		case r.URL.Path == "/v1/streams/workspace-test/backups/delete" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(ServiceDeleteResult{
				StreamID: "workspace-test", DeletedIDs: []string{fixture.backup.ID}, DeletedCount: 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func TestProviderHTTPHandlersOwnLifecycle(t *testing.T) {
	service := newBackupHTTPServiceFixture(t)
	database, databasePath := openProviderTestDatabase(t)
	scope := providerTestScope(database, databasePath)
	auditActions := []string{}
	baseMutation := scope.Mutate
	scope.Mutate = func(ctx context.Context, action string, payload func() any, mutate func(tx *sql.Tx) error) error {
		auditActions = append(auditActions, action)
		return baseMutation(ctx, action, payload, mutate)
	}
	scope.AuthorizePassword = func(_ http.ResponseWriter, _ *http.Request, password string) bool {
		return password == "M7!river-Quartz_92fox"
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))

	createdResponse := performBackupJSON(t, handlers.CreateProvider, http.MethodPost, "/", nil, backupProviderRequest{
		ProviderType: ServiceProviderType, Name: "Remote backup",
		Public: map[string]any{"base_url": service.server.URL},
		Secret: map[string]any{"token": testOldServiceToken},
	})
	if createdResponse.Code != http.StatusCreated || strings.Contains(createdResponse.Body.String(), testOldServiceToken) {
		t.Fatalf("create response=%d %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created ProviderResponse
	decodeBackupResponse(t, createdResponse, &created)
	if created.Status != "disabled" || !created.HasSecret {
		t.Fatalf("created provider=%#v", created)
	}

	listResponse := httptest.NewRecorder()
	handlers.ListProviders(listResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), "Remote backup") {
		t.Fatalf("list response=%d %s", listResponse.Code, listResponse.Body.String())
	}

	activeUpdate := performBackupJSON(t, handlers.UpdateProvider, http.MethodPut, "/", map[string]string{
		"id": strconv.FormatInt(created.ID, 10),
	}, backupProviderRequest{Status: "active"})
	if activeUpdate.Code != http.StatusConflict {
		t.Fatalf("implicit enable response=%d %s", activeUpdate.Code, activeUpdate.Body.String())
	}
	updateResponse := performBackupJSON(t, handlers.UpdateProvider, http.MethodPut, "/", map[string]string{
		"id": strconv.FormatInt(created.ID, 10),
	}, backupProviderRequest{Name: "Remote backup renamed", Status: "disabled", Public: map[string]any{
		"base_url": service.server.URL,
	}})
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), "Remote backup renamed") {
		t.Fatalf("update response=%d %s", updateResponse.Code, updateResponse.Body.String())
	}

	testResponse := performBackupJSON(t, handlers.TestProvider, http.MethodPost, "/", map[string]string{
		"id": strconv.FormatInt(created.ID, 10),
	}, nil)
	if testResponse.Code != http.StatusOK || !strings.Contains(testResponse.Body.String(), "\"service_version\":\"fixture\"") {
		t.Fatalf("test response=%d %s", testResponse.Code, testResponse.Body.String())
	}
	enableResponse := performBackupJSON(t, handlers.EnableProvider, http.MethodPost, "/", map[string]string{
		"id": strconv.FormatInt(created.ID, 10),
	}, enableBackupProviderRequest{CurrentPassword: "M7!river-Quartz_92fox"})
	if enableResponse.Code != http.StatusOK || !strings.Contains(enableResponse.Body.String(), "\"status\":\"active\"") {
		t.Fatalf("enable response=%d %s", enableResponse.Code, enableResponse.Body.String())
	}

	activeUpdate = performBackupJSON(t, handlers.UpdateProvider, http.MethodPut, "/", map[string]string{
		"id": strconv.FormatInt(created.ID, 10),
	}, backupProviderRequest{Name: "Active remote backup", Status: "active", Public: map[string]any{
		"base_url": service.server.URL,
	}})
	if activeUpdate.Code != http.StatusOK {
		t.Fatalf("active update response=%d %s", activeUpdate.Code, activeUpdate.Body.String())
	}

	deleteResponse := performBackupJSON(t, handlers.DeleteProvider, http.MethodDelete, "/", map[string]string{
		"id": strconv.FormatInt(created.ID, 10),
	}, nil)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete response=%d %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if strings.Join(auditActions, ",") != "backup.provider.created,backup.provider.updated,backup.provider.tested,backup.provider.enabled,backup.provider.updated,backup.provider.archived" {
		t.Fatalf("audit actions=%v", auditActions)
	}
}

func TestProviderHTTPHandlersOwnRecordsAndRetention(t *testing.T) {
	service := newBackupHTTPServiceFixture(t)
	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.server.URL, testOldServiceToken, "active")
	scope := providerTestScope(database, databasePath)
	requiredActions := []string{}
	observedActions := []string{}
	scope.AuditRequired = func(_ context.Context, action string, _ any) error {
		requiredActions = append(requiredActions, action)
		return nil
	}
	scope.Observe = func(_ context.Context, action string, _ any) {
		observedActions = append(observedActions, action)
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))
	pathValues := map[string]string{"id": strconv.FormatInt(provider.ID, 10)}

	recordsResponse := performBackupJSON(t, handlers.ListProviderRecords, http.MethodGet, "/", pathValues, nil)
	if recordsResponse.Code != http.StatusOK || !strings.Contains(recordsResponse.Body.String(), service.backup.ID) {
		t.Fatalf("records response=%d %s", recordsResponse.Code, recordsResponse.Body.String())
	}
	records, err := NewStore(database).ListRecords(t.Context(), ListRecordsFilter{ProviderID: provider.ID})
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%#v error=%v", records, err)
	}

	freshnessResponse := httptest.NewRecorder()
	handlers.BackupFreshness(freshnessResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if freshnessResponse.Code != http.StatusOK || !strings.Contains(freshnessResponse.Body.String(), "\"remote_newer\":true") {
		t.Fatalf("freshness response=%d %s", freshnessResponse.Code, freshnessResponse.Body.String())
	}

	downloadValues := map[string]string{
		"id": strconv.FormatInt(provider.ID, 10), "record_id": strconv.FormatInt(records[0].ID, 10),
	}
	downloadResponse := performBackupJSON(t, handlers.DownloadProviderRecord, http.MethodGet, "/", downloadValues, nil)
	if downloadResponse.Code != http.StatusOK || !bytes.Equal(downloadResponse.Body.Bytes(), service.payload) {
		t.Fatalf("download response=%d body=%q", downloadResponse.Code, downloadResponse.Body.Bytes())
	}

	storageResponse := performBackupJSON(t, handlers.BackupProviderStorage, http.MethodGet, "/", pathValues, nil)
	if storageResponse.Code != http.StatusOK || !strings.Contains(storageResponse.Body.String(), "\"used_bytes\":1024") {
		t.Fatalf("storage response=%d %s", storageResponse.Code, storageResponse.Body.String())
	}
	retentionResponse := performBackupJSON(t, handlers.BackupProviderRetention, http.MethodGet, "/", pathValues, nil)
	if retentionResponse.Code != http.StatusOK || !strings.Contains(retentionResponse.Body.String(), "\"keep_latest\":10") {
		t.Fatalf("retention response=%d %s", retentionResponse.Code, retentionResponse.Body.String())
	}
	previewResponse := performBackupJSON(t, handlers.PreviewBackupProviderRetention, http.MethodPost, "/", pathValues,
		pruneBackupProviderRequest{KeepLatest: 5})
	if previewResponse.Code != http.StatusOK || !strings.Contains(previewResponse.Body.String(), "\"keep_latest\":5") {
		t.Fatalf("preview response=%d %s", previewResponse.Code, previewResponse.Body.String())
	}
	updateResponse := performBackupJSON(t, handlers.UpdateBackupProviderRetention, http.MethodPut, "/", pathValues,
		updateBackupRetentionRequest{Enabled: true, KeepLatest: 5, ApplyNow: true})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("retention update response=%d %s", updateResponse.Code, updateResponse.Body.String())
	}
	pruneResponse := performBackupJSON(t, handlers.PruneProviderBackups, http.MethodPost, "/", pathValues,
		pruneBackupProviderRequest{KeepLatest: 5})
	if pruneResponse.Code != http.StatusOK {
		t.Fatalf("prune response=%d %s", pruneResponse.Code, pruneResponse.Body.String())
	}
	deleteResponse := performBackupJSON(t, handlers.DeleteProviderBackupRecords, http.MethodDelete, "/", pathValues,
		deleteBackupRecordsRequest{RecordIDs: []int64{records[0].ID}})
	if deleteResponse.Code != http.StatusOK || !strings.Contains(deleteResponse.Body.String(), "\"deleted_count\":1") {
		t.Fatalf("record delete response=%d %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if strings.Join(requiredActions, ",") != "backup.provider.retention.update_requested,backup.provider.prune_requested,backup.provider.records.delete_requested" {
		t.Fatalf("required audit actions=%v", requiredActions)
	}
	if strings.Join(observedActions, ",") != "backup.provider.record.downloaded,backup.provider.retention.updated,backup.provider.pruned" {
		t.Fatalf("observed actions=%v", observedActions)
	}
}

func TestCreateDatabaseSnapshotCopiesEncryptedWorkspace(t *testing.T) {
	database, databasePath := openProviderTestDatabase(t)
	snapshot, err := CreateDatabaseSnapshot(t.Context(), SnapshotSource{
		Database: database, DatabaseID: "db-test", Path: databasePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(snapshot.Path) })
	if snapshot.Path == databasePath || !strings.HasSuffix(snapshot.Filename, ".aipdb") {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	info, err := os.Stat(snapshot.Path)
	if err != nil || info.Size() < 1 {
		t.Fatalf("snapshot info=%v error=%v", info, err)
	}
	relativePath, err := filepath.Rel(filepath.Dir(databasePath), snapshot.Path)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		t.Fatalf("snapshot escaped database directory: %q", snapshot.Path)
	}
}

func performBackupJSON(
	t *testing.T,
	handler http.HandlerFunc,
	method, path string,
	pathValues map[string]string,
	payload any,
) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range pathValues {
		request.SetPathValue(name, value)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func decodeBackupResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response: %v body=%s", err, response.Body.String())
	}
}
