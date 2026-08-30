package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/config"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

const backupAPITestPassword = "M7!river-Quartz_92fox"
const backupAPITestToken = "backup-api-test-token-with-more-than-thirty-two-characters"

func assertAuditActionCount(t *testing.T, database *sql.DB, action string, want int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = ?`, action).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("audit action %s count=%d, want %d", action, count, want)
	}
}

type backupAPITestFixture struct {
	server *Server
	db     *sql.DB
}

type fakeBackupService struct {
	server    *httptest.Server
	mu        sync.Mutex
	item      backups.ServiceBackup
	data      []byte
	listCalls int
	retention backups.ServiceRetentionPolicy
}

func TestBackupProviderLifecycleUsesEncryptedTokenAndImmutableVersions(t *testing.T) {
	remote := newFakeBackupService(t)
	fixture := newBackupAPITestFixture(t, backupAPITestPassword)
	handler := fixture.server.Handler()

	catalog := performJSON(handler, http.MethodGet, "/api/backup/providers/catalog", "", nil)
	if catalog.Code != http.StatusOK || !strings.Contains(catalog.Body.String(), backups.ServiceProviderType) {
		t.Fatalf("catalog failed: %d %s", catalog.Code, catalog.Body.String())
	}
	create := performJSON(handler, http.MethodPost, "/api/backup/providers", "", map[string]any{
		"provider_type": backups.ServiceProviderType,
		"name":          "Private backup service",
		"public":        map[string]any{"base_url": remote.server.URL},
		"secret":        map[string]any{"token": backupAPITestToken},
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", create.Code, create.Body.String())
	}
	created := decodeRouteResponse[backupProviderResponse](t, create.Body.Bytes())
	if created.Status != "disabled" || !created.HasSecret || strings.Contains(create.Body.String(), backupAPITestToken) {
		t.Fatalf("provider did not start safely or leaked token: %s", create.Body.String())
	}
	var encrypted string
	if err := fixture.db.QueryRow(`SELECT encrypted_secret_json FROM backup_providers WHERE id = ?`, created.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if encrypted == "" || strings.Contains(encrypted, backupAPITestToken) {
		t.Fatalf("service token was not encrypted: %q", encrypted)
	}

	testResponse := performJSON(handler, http.MethodPost, providerPath(created.ID, "/test"), "", map[string]any{})
	if testResponse.Code != http.StatusOK || !strings.Contains(testResponse.Body.String(), `"protocol_version":"`+backups.ServiceProtocol+`"`) {
		t.Fatalf("provider test failed: %d %s", testResponse.Code, testResponse.Body.String())
	}
	wrongPassword := performJSON(handler, http.MethodPost, providerPath(created.ID, "/enable"), "", enableBackupProviderRequest{CurrentPassword: "wrong-password"})
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("wrong enable password should fail: %d %s", wrongPassword.Code, wrongPassword.Body.String())
	}
	enable := performJSON(handler, http.MethodPost, providerPath(created.ID, "/enable"), "", enableBackupProviderRequest{CurrentPassword: backupAPITestPassword})
	if enable.Code != http.StatusOK || !strings.Contains(enable.Body.String(), `"status":"active"`) {
		t.Fatalf("enable failed: %d %s", enable.Code, enable.Body.String())
	}

	upload := performJSON(handler, http.MethodPost, providerPath(created.ID, "/upload"), "", map[string]any{})
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d %s", upload.Code, upload.Body.String())
	}
	record := decodeRouteResponse[backupRecordResponse](t, upload.Body.Bytes())
	if record.ProviderFileID == "" || record.ChecksumSHA256 == "" || record.SizeBytes < 1 {
		t.Fatalf("upload metadata incomplete: %#v", record)
	}
	provider, err := backups.NewStore(fixture.db).GetProvider(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := backups.ReadServiceBaseline(context.Background(), fixture.db, remote.server.URL, stringFromMap(provider.Public, "stream_id"))
	if err != nil || baseline == nil || baseline.BackupID != record.ProviderFileID {
		t.Fatalf("upload did not advance the encrypted local baseline: %#v err=%v", baseline, err)
	}
	assertAuditActionCount(t, fixture.db, "backup.provider.uploaded", 1)

	list := performJSON(handler, http.MethodGet, providerPath(created.ID, "/records"), "", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), record.ProviderFileID) || !strings.Contains(list.Body.String(), `"remote_sync":true`) {
		t.Fatalf("record sync failed: %d %s", list.Code, list.Body.String())
	}
	storage := performJSON(handler, http.MethodGet, providerPath(created.ID, "/storage"), "", nil)
	if storage.Code != http.StatusOK {
		t.Fatalf("storage usage failed: %d %s", storage.Code, storage.Body.String())
	}
	storageUsage := decodeRouteResponse[backups.ServiceStorageUsage](t, storage.Body.Bytes())
	if storageUsage.UsedBytes < 1 || !storageUsage.QuotaEnabled || storageUsage.BackupCount != 1 {
		t.Fatalf("storage usage was incomplete: %#v", storageUsage)
	}
	retention := performJSON(handler, http.MethodGet, providerPath(created.ID, "/retention"), "", nil)
	if retention.Code != http.StatusOK {
		t.Fatalf("retention policy failed: %d %s", retention.Code, retention.Body.String())
	}
	retentionPolicy := decodeRouteResponse[backups.ServiceRetentionPolicy](t, retention.Body.Bytes())
	if retentionPolicy.Enabled || retentionPolicy.KeepLatest != 0 {
		t.Fatalf("unexpected default retention policy: %#v", retentionPolicy)
	}
	if invalid := performJSON(handler, http.MethodPost, providerPath(created.ID, "/retention/preview"), "", map[string]any{"keep_latest": 0}); invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid retention preview status = %d body=%s", invalid.Code, invalid.Body.String())
	}
	if invalid := performJSON(handler, http.MethodPut, providerPath(created.ID, "/retention"), "", updateBackupRetentionRequest{Enabled: false, KeepLatest: 1}); invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid disabled retention status = %d body=%s", invalid.Code, invalid.Body.String())
	}
	preview := performJSON(handler, http.MethodPost, providerPath(created.ID, "/retention/preview"), "", map[string]any{"keep_latest": 1})
	if preview.Code != http.StatusOK {
		t.Fatalf("retention preview failed: %d %s", preview.Code, preview.Body.String())
	}
	retentionPreview := decodeRouteResponse[backups.ServiceRetentionPreview](t, preview.Body.Bytes())
	if retentionPreview.KeepLatest != 1 || retentionPreview.RetainCount != 1 || retentionPreview.DeleteCount != 0 {
		t.Fatalf("unexpected retention preview: %#v", retentionPreview)
	}
	update := performJSON(handler, http.MethodPut, providerPath(created.ID, "/retention"), "", updateBackupRetentionRequest{
		Enabled: true, KeepLatest: 1, ApplyNow: true,
	})
	if update.Code != http.StatusOK {
		t.Fatalf("retention update failed: %d %s", update.Code, update.Body.String())
	}
	retentionUpdate := decodeRouteResponse[backups.ServiceRetentionUpdate](t, update.Body.Bytes())
	if !retentionUpdate.Policy.Enabled || retentionUpdate.Policy.KeepLatest != 1 || retentionUpdate.DeletedCount != 0 {
		t.Fatalf("unexpected retention update: %#v", retentionUpdate)
	}
	prune := performJSON(handler, http.MethodPost, providerPath(created.ID, "/prune"), "", pruneBackupProviderRequest{KeepLatest: 1})
	if prune.Code != http.StatusOK {
		t.Fatalf("prune failed: %d %s", prune.Code, prune.Body.String())
	}
	pruneResult := decodeRouteResponse[backups.ServicePruneResult](t, prune.Body.Bytes())
	if pruneResult.KeepLatest != 1 || pruneResult.DeletedCount != 0 {
		t.Fatalf("unexpected prune result: %#v", pruneResult)
	}
	download := performJSON(handler, http.MethodGet, providerPath(created.ID, "/records/"+strconv.FormatInt(record.ID, 10)+"/download"), "", nil)
	if download.Code != http.StatusOK || download.Body.Len() != int(record.SizeBytes) {
		t.Fatalf("download failed: %d bytes=%d body=%s", download.Code, download.Body.Len(), download.Body.String())
	}
	remote.mu.Lock()
	listCallsBeforeDelete := remote.listCalls
	remote.mu.Unlock()
	deleteRecords := performJSON(handler, http.MethodPost, providerPath(created.ID, "/records/delete"), "", deleteBackupRecordsRequest{RecordIDs: []int64{record.ID}})
	if deleteRecords.Code != http.StatusOK {
		t.Fatalf("selected delete failed: %d %s", deleteRecords.Code, deleteRecords.Body.String())
	}
	deleteResult := decodeRouteResponse[backups.ServiceDeleteResult](t, deleteRecords.Body.Bytes())
	if deleteResult.DeletedCount != 1 || len(deleteResult.DeletedIDs) != 1 || deleteResult.DeletedIDs[0] != record.ProviderFileID {
		t.Fatalf("unexpected selected delete result: %#v", deleteResult)
	}
	remote.mu.Lock()
	listCallsAfterDelete := remote.listCalls
	remote.mu.Unlock()
	if listCallsAfterDelete != listCallsBeforeDelete {
		t.Fatal("selected delete performed a second remote request after the confirmed deletion")
	}
	assertAuditActionCount(t, fixture.db, "backup.provider.records.deleted", 1)
	list = performJSON(handler, http.MethodGet, providerPath(created.ID, "/records"), "", nil)
	listed := decodeRouteResponse[struct {
		Items []backupRecordResponse `json:"items"`
	}](t, list.Body.Bytes())
	if list.Code != http.StatusOK || len(listed.Items) != 0 {
		t.Fatalf("deleted record was not reconciled: %d %s", list.Code, list.Body.String())
	}

	disable := performJSON(handler, http.MethodPut, providerPath(created.ID, ""), "", map[string]any{
		"name": "Private backup service", "status": "disabled",
	})
	if disable.Code != http.StatusOK || !strings.Contains(disable.Body.String(), `"status":"disabled"`) {
		t.Fatalf("disable failed: %d %s", disable.Code, disable.Body.String())
	}
	implicitEnable := performJSON(handler, http.MethodPut, providerPath(created.ID, ""), "", map[string]any{
		"name": "Private backup service", "status": "active",
	})
	if implicitEnable.Code != http.StatusConflict {
		t.Fatalf("implicit enable should fail: %d %s", implicitEnable.Code, implicitEnable.Body.String())
	}

	deleted := performJSON(handler, http.MethodDelete, providerPath(created.ID, ""), "", nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete failed: %d %s", deleted.Code, deleted.Body.String())
	}
	if err := fixture.db.QueryRow(`SELECT encrypted_secret_json FROM backup_providers WHERE id = ?`, created.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if encrypted != "" {
		t.Fatal("archived provider retained its encrypted token")
	}
}

func TestRemoteBackupFreshnessUsesKnownVersionAndTimestamp(t *testing.T) {
	remote := backups.ServiceBackup{ID: "bkp_new", CreatedAt: "2026-07-31T12:00:00Z"}
	if !remoteBackupIsNewer(remote, nil) {
		t.Fatal("a remote version without a local baseline should be reported")
	}
	baseline := &backups.ServiceBaseline{BackupID: "bkp_old", CreatedAt: "2026-07-31T11:00:00Z"}
	if !remoteBackupIsNewer(remote, baseline) {
		t.Fatal("newer remote version was not reported")
	}
	baseline = &backups.ServiceBaseline{BackupID: "bkp_new", CreatedAt: "2026-07-31T12:00:00Z"}
	if remoteBackupIsNewer(remote, baseline) {
		t.Fatal("the known remote version should not be reported as newer")
	}
	baseline = &backups.ServiceBaseline{BackupID: "bkp_future", CreatedAt: "2026-07-31T13:00:00Z"}
	if remoteBackupIsNewer(remote, baseline) {
		t.Fatal("an older remote version should not be reported as newer")
	}
}

func TestBackupProviderRoutesRejectUnsupportedAndLockedRequests(t *testing.T) {
	fixture := newBackupAPITestFixture(t, backupAPITestPassword)
	unsupported := performJSON(fixture.server.Handler(), http.MethodPost, "/api/backup/providers", "", map[string]any{
		"provider_type": "google_drive", "name": "Old provider",
	})
	if unsupported.Code != http.StatusBadRequest || !strings.Contains(unsupported.Body.String(), "unsupported") {
		t.Fatalf("unsupported provider was accepted: %d %s", unsupported.Code, unsupported.Body.String())
	}
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	if response := performJSON(locked.Handler(), http.MethodGet, "/api/backup/providers", "", nil); response.Code != http.StatusLocked {
		t.Fatalf("locked provider list should fail: %d %s", response.Code, response.Body.String())
	}
}

func TestDatabasePasswordChangeRequiresRemoteBackupStrengthWhileProviderActive(t *testing.T) {
	fixture := newBackupAPITestFixture(t, backupAPITestPassword)
	store := backups.NewStore(fixture.db)
	if _, err := store.CreateProvider(context.Background(), backups.CreateProviderRequest{
		ProviderType: backups.ServiceProviderType, Name: "Active", Status: "active",
		Public: map[string]any{"base_url": "https://backup.example.com"}, Encrypted: "ciphertext",
	}); err != nil {
		t.Fatal(err)
	}
	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/databases/change-password", "", changeDatabasePasswordRequest{
		CurrentPassword: backupAPITestPassword,
		NewPassword:     "BasicPassword1234",
		ConfirmPassword: "BasicPassword1234",
	})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "remote backup") {
		t.Fatalf("weak remote backup password was accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestFirstRunRemoteRestoreKeepsCredentialsTransient(t *testing.T) {
	remote := newFakeBackupService(t)
	sourcePath := filepath.Join(t.TempDir(), "remote.aipdb")
	sourceDatabase, err := dbpkg.OpenEncrypted(sourcePath, backupAPITestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	remote.mu.Lock()
	remote.data = data
	remote.item = backups.ServiceBackup{
		ID: "bkp_restore", StreamID: "workspace-restore", DatabaseName: "Recovered Project",
		SourceInstallationID: "install-restore", Filename: "recovered-project.aipdb",
		SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	remote.mu.Unlock()

	dataPath := filepath.Join(t.TempDir(), "aipermission.aipdb")
	server := NewLockedServer(config.Config{
		Host: "127.0.0.1", Port: "8080", DataPath: dataPath,
		GatewaySecret: "first-run-test-gateway-secret", AllowedOrigins: []string{"http://localhost:3001"},
	})
	t.Cleanup(server.Close)
	list := performJSON(server.Handler(), http.MethodPost, "/api/backup/remote/list", "", transientBackupServiceRequest{
		BaseURL: remote.server.URL, Token: backupAPITestToken,
	})
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "Recovered Project") || strings.Contains(list.Body.String(), backupAPITestToken) {
		t.Fatalf("transient list failed or leaked token: %d %s", list.Code, list.Body.String())
	}
	versions := performJSON(server.Handler(), http.MethodPost, "/api/backup/remote/list", "", transientBackupServiceRequest{
		BaseURL: remote.server.URL, Token: backupAPITestToken, StreamID: "workspace-restore",
	})
	if versions.Code != http.StatusOK || !strings.Contains(versions.Body.String(), "bkp_restore") || strings.Contains(versions.Body.String(), backupAPITestToken) {
		t.Fatalf("transient version list failed or leaked token: %d %s", versions.Code, versions.Body.String())
	}
	restore := performJSON(server.Handler(), http.MethodPost, "/api/backup/remote/restore", "", transientBackupRestoreRequest{
		BaseURL: remote.server.URL, Token: backupAPITestToken, StreamID: "workspace-restore",
		BackupID: "bkp_restore", DatabaseName: "Restored Copy", DatabasePassword: backupAPITestPassword,
	})
	if restore.Code != http.StatusOK || !strings.Contains(restore.Body.String(), `"state":"unlocked"`) || !strings.Contains(restore.Body.String(), `"database_id":"restored-copy"`) {
		t.Fatalf("transient restore failed: %d %s", restore.Code, restore.Body.String())
	}
	var providerCount int
	if err := server.activeRuntime().database.QueryRow(`SELECT COUNT(*) FROM backup_providers`).Scan(&providerCount); err != nil {
		t.Fatal(err)
	}
	if providerCount != 0 {
		t.Fatal("first-run credentials were persisted as a provider")
	}
	baseline, err := backups.ReadServiceBaseline(context.Background(), server.activeRuntime().database, remote.server.URL, "workspace-restore")
	if err != nil || baseline == nil || baseline.BackupID != "bkp_restore" {
		t.Fatalf("restored database did not retain its remote baseline: %#v err=%v", baseline, err)
	}
}

func newBackupAPITestFixture(t *testing.T, password string) backupAPITestFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aipermission.aipdb")
	database, err := dbpkg.OpenEncrypted(path, password)
	if err != nil {
		t.Fatal(err)
	}
	secretVault, err := vault.New("backup-api-test-gateway-secret")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(config.Config{
		Host: "127.0.0.1", Port: "8080", DataPath: path,
		GatewaySecret: "backup-api-test-gateway-secret", AllowedOrigins: []string{"http://localhost:3001"},
	}, database, secretVault, tokens.NewStore(database))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	authorizeTestUISession(srv)
	t.Cleanup(func() { srv.Close() })
	return backupAPITestFixture{server: srv, db: database}
}

func newFakeBackupService(t *testing.T) *fakeBackupService {
	t.Helper()
	service := &fakeBackupService{}
	service.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+backupAPITestToken {
			writeFakeServiceError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if r.URL.Path != "/v1/info" && r.Header.Get("X-AIPermission-Protocol-Version") != backups.ServiceProtocol {
			writeFakeServiceError(w, http.StatusUpgradeRequired, "protocol_mismatch")
			return
		}
		service.mu.Lock()
		defer service.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/info":
			json.NewEncoder(w).Encode(backups.ServiceInfo{
				Service:         "aipermission-backup",
				Version:         "test",
				ProtocolVersion: backups.ServiceProtocol,
				Capabilities: []string{
					"immutable_upload",
					"list_streams",
					"list_versions",
					"download",
					"prune_versions",
					"delete_versions",
					"storage_usage",
					"automatic_retention",
				},
				MaxUploadBytes: maxImportBodyBytes,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/streams":
			items := []backups.ServiceStream{}
			if service.item.ID != "" {
				items = append(items, backups.ServiceStream{ID: service.item.StreamID, DatabaseName: service.item.DatabaseName})
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items, "next_cursor": ""})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/storage":
			remaining := int64(16<<20) - int64(len(service.data))
			json.NewEncoder(w).Encode(backups.ServiceStorageUsage{
				UsedBytes: int64(len(service.data)), QuotaEnabled: true, QuotaBytes: 16 << 20,
				RemainingBytes: &remaining, BackupCount: testBoolToInt64(service.item.ID != ""), StreamCount: 1,
			})
		case strings.HasSuffix(r.URL.Path, "/retention") && r.Method == http.MethodGet:
			policy := service.retention
			if policy.StreamID == "" {
				policy.StreamID = service.item.StreamID
			}
			json.NewEncoder(w).Encode(policy)
		case strings.HasSuffix(r.URL.Path, "/retention/preview") && r.Method == http.MethodPost:
			json.NewEncoder(w).Encode(backups.ServiceRetentionPreview{
				StreamID: service.item.StreamID, KeepLatest: 1, RetainCount: testBoolToInt(service.item.ID != ""), RetainBytes: int64(len(service.data)),
			})
		case strings.HasSuffix(r.URL.Path, "/retention") && r.Method == http.MethodPut:
			service.retention = backups.ServiceRetentionPolicy{StreamID: service.item.StreamID, Enabled: true, KeepLatest: 1}
			json.NewEncoder(w).Encode(backups.ServiceRetentionUpdate{
				Policy:  service.retention,
				Preview: backups.ServiceRetentionPreview{StreamID: service.item.StreamID, KeepLatest: 1, RetainCount: testBoolToInt(service.item.ID != ""), RetainBytes: int64(len(service.data))},
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/streams/") && strings.HasSuffix(r.URL.Path, "/backups"):
			data, err := io.ReadAll(r.Body)
			if err != nil || len(data) == 0 {
				writeFakeServiceError(w, http.StatusBadRequest, "invalid_backup")
				return
			}
			digest := sha256.Sum256(data)
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			service.data = data
			service.item = backups.ServiceBackup{
				ID: "bkp_test", StreamID: parts[2], DatabaseName: r.Header.Get("X-AIPermission-Database-Name"),
				SourceInstallationID: r.Header.Get("X-AIPermission-Source-Installation-ID"), Filename: "test-database.aipdb",
				SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(service.item)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/backups"):
			service.listCalls++
			items := []backups.ServiceBackup{}
			if service.item.ID != "" {
				items = append(items, service.item)
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items, "next_cursor": ""})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/prune"):
			json.NewEncoder(w).Encode(backups.ServicePruneResult{StreamID: service.item.StreamID, KeepLatest: 1})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/backups/delete"):
			streamID := service.item.StreamID
			deletedID := service.item.ID
			service.item = backups.ServiceBackup{}
			service.data = nil
			json.NewEncoder(w).Encode(backups.ServiceDeleteResult{StreamID: streamID, DeletedIDs: []string{deletedID}, DeletedCount: 1})
		case r.Method == http.MethodGet && service.item.ID != "" && strings.HasSuffix(r.URL.Path, "/backups/"+service.item.ID):
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="test-database.aipdb"`)
			w.Header().Set("X-AIPermission-Backup-ID", service.item.ID)
			w.Header().Set("X-AIPermission-SHA256", service.item.SHA256)
			w.Write(service.data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(service.server.Close)
	return service
}

func testBoolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func testBoolToInt64(value bool) int64 {
	return int64(testBoolToInt(value))
}

func providerPath(id int64, suffix string) string {
	return "/api/backup/providers/" + strconv.FormatInt(id, 10) + suffix
}

func writeFakeServiceError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": code}})
}
