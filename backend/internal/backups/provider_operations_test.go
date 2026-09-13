package backups

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const (
	testOldServiceToken = "old-backup-token-1234567890-abcdef"
	testNewServiceToken = "new-backup-token-1234567890-abcdef"
)

type testProviderSecretCodec struct{}

func (testProviderSecretCodec) EncryptProviderSecret(_ int64, secret map[string]any) (string, error) {
	token, _ := secret["token"].(string)
	return token, nil
}

func (testProviderSecretCodec) DecryptProviderSecret(provider Provider) (map[string]any, error) {
	return map[string]any{"token": provider.EncryptedSecretJSON}, nil
}

func TestQueuedUploadReloadsRotatedProviderCredential(t *testing.T) {
	var receivedAuthorization string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upload: %v", err)
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		digest := sha256.Sum256(data)
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(ServiceBackup{
			ID: "backup-new", StreamID: parts[2], DatabaseName: r.Header.Get("X-AIPermission-Database-Name"),
			SourceInstallationID: r.Header.Get("X-AIPermission-Source-Installation-ID"), Filename: "database.aipdb",
			SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
	}))
	t.Cleanup(service.Close)

	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.aipdb")
	if err := os.WriteFile(snapshotPath, []byte("encrypted snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	acquireStarted := make(chan struct{})
	allowAcquire := make(chan struct{})
	scope := providerTestScope(database, databasePath)
	acquireOperation := func(ctx context.Context) (func(), error) {
		close(acquireStarted)
		select {
		case <-allowAcquire:
			return func() {}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	scope.CreateSnapshot = func(context.Context) (DatabaseSnapshot, error) {
		return DatabaseSnapshot{Path: snapshotPath}, nil
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, acquireOperation))
	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", strings.NewReader(`{"idempotency_key":"queued-upload"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handlers.UploadProviderBackup(response, request)
	}()

	waitForProviderTestSignal(t, acquireStarted, "upload operation acquisition")
	rotated := testNewServiceToken
	if _, err := NewStore(database).UpdateProvider(context.Background(), provider.ID, UpdateProviderRequest{
		Name: provider.Name, Status: provider.Status, Public: provider.Public, Encrypted: &rotated,
	}); err != nil {
		t.Fatal(err)
	}
	close(allowAcquire)
	waitForProviderTestSignal(t, done, "queued upload completion")

	if response.Code != http.StatusCreated {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if receivedAuthorization != "Bearer "+testNewServiceToken {
		t.Fatalf("authorization = %q, want rotated credential", receivedAuthorization)
	}
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("snapshot was not removed after upload: %v", err)
	}
}

func TestUploadRetryReconcilesOneRemoteVersionAfterUncertainResults(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		loseFirstResponse bool
		failFirstMutation bool
	}{
		{name: "remote response loss", loseFirstResponse: true},
		{name: "local commit failure", failFirstMutation: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			payload := []byte("first encrypted snapshot")
			digest := sha256.Sum256(payload)
			backup := ServiceBackup{
				ID: "backup-stable", StreamID: "workspace-test", DatabaseName: "Test Database",
				SourceInstallationID: backupSourceInstallationID(t.TempDir()), Filename: "database.aipdb",
				SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			var calls, committed atomic.Int64
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if committed.CompareAndSwap(0, 1) {
					body, err := io.ReadAll(r.Body)
					if err != nil || !bytes.Equal(body, payload) {
						t.Errorf("unexpected first upload: body=%q err=%v", body, err)
						return
					}
					backup.SourceInstallationID = r.Header.Get("X-AIPermission-Source-Installation-ID")
					if testCase.loseFirstResponse {
						w.WriteHeader(http.StatusCreated)
						_, _ = w.Write([]byte(`{"id":`))
						return
					}
					w.WriteHeader(http.StatusCreated)
				} else {
					w.WriteHeader(http.StatusOK)
				}
				_ = json.NewEncoder(w).Encode(backup)
			}))
			t.Cleanup(service.Close)

			database, databasePath := openProviderTestDatabase(t)
			provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
			scope := providerTestScope(database, databasePath)
			var snapshots atomic.Int64
			scope.CreateSnapshot = func(context.Context) (DatabaseSnapshot, error) {
				contents := payload
				if snapshots.Add(1) > 1 {
					contents = []byte("newer local snapshot after response uncertainty")
				}
				path := filepath.Join(t.TempDir(), "snapshot.aipdb")
				if err := os.WriteFile(path, contents, 0o600); err != nil {
					return DatabaseSnapshot{}, err
				}
				return DatabaseSnapshot{Path: path}, nil
			}
			if testCase.failFirstMutation {
				var mutations atomic.Int64
				scope.Mutate = func(ctx context.Context, _ string, payload func() any, mutate func(*sql.Tx) error) error {
					tx, err := database.BeginTx(ctx, nil)
					if err != nil {
						return err
					}
					defer tx.Rollback()
					if err := mutate(tx); err != nil {
						return err
					}
					_ = payload()
					if mutations.Add(1) == 1 {
						return errors.New("injected local commit failure")
					}
					return tx.Commit()
				}
			}
			handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))
			invoke := func() *httptest.ResponseRecorder {
				request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", strings.NewReader(`{"idempotency_key":"stable-upload"}`))
				request.Header.Set("Content-Type", "application/json")
				request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
				response := httptest.NewRecorder()
				handlers.UploadProviderBackup(response, request)
				return response
			}
			if first := invoke(); first.Code < 400 {
				t.Fatalf("first uncertain attempt unexpectedly succeeded: %d %s", first.Code, first.Body.String())
			}
			operation, err := NewStore(database).GetUploadOperation(context.Background(), "stable-upload")
			if err != nil || operation.Status != "outcome_unknown" {
				t.Fatalf("uncertain operation was not preserved: operation=%#v err=%v", operation, err)
			}
			if second := invoke(); second.Code != http.StatusCreated {
				t.Fatalf("retry failed: %d %s", second.Code, second.Body.String())
			}
			operation, err = NewStore(database).GetUploadOperation(context.Background(), "stable-upload")
			if err != nil || operation.Status != "completed" || operation.ProviderFileID != backup.ID {
				t.Fatalf("operation was not reconciled: operation=%#v err=%v", operation, err)
			}
			if committed.Load() != 1 || calls.Load() != 2 {
				t.Fatalf("remote versions=%d calls=%d, want one version across two calls", committed.Load(), calls.Load())
			}
		})
	}
}

func TestUploadOperationCompletionIsIdempotentForTheSameRemoteBackup(t *testing.T) {
	database, _ := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, "http://127.0.0.1:1", testOldServiceToken, "active")
	store := NewStore(database)
	_, _, err := store.ClaimUploadOperation(context.Background(), ClaimUploadOperationRequest{
		IdempotencyKey: "completion-race", ProviderID: provider.ID, DatabaseID: "db-test",
		StreamID: "workspace-test", SourceInstallationID: "install-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteUploadOperation(context.Background(), "completion-race", "backup-stable"); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteUploadOperation(context.Background(), "completion-race", "backup-stable"); err != nil {
		t.Fatalf("same completion was not idempotent: %v", err)
	}
	if err := store.CompleteUploadOperation(context.Background(), "completion-race", "backup-different"); err == nil {
		t.Fatal("different remote backup unexpectedly reused a completed operation")
	}
}

func TestQueuedDownloadRejectsProviderDisabledWhileWaiting(t *testing.T) {
	var remoteCalls atomic.Int64
	service := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		remoteCalls.Add(1)
	}))
	t.Cleanup(service.Close)
	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
	record, err := NewStore(database).UpsertRecord(context.Background(), CreateRecordRequest{
		ProviderID: provider.ID, DatabaseID: "db-test", DatabaseName: "Test Database",
		ProviderFileID: "backup-old", Filename: "database.aipdb", SizeBytes: 12,
		ChecksumSHA256: strings.Repeat("a", 64), BackupCreatedAt: "2026-09-10T10:00:00Z", UploadedAt: "2026-09-10T10:00:00Z",
		Metadata: map[string]any{"stream_id": "workspace-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	acquireStarted := make(chan struct{})
	allowAcquire := make(chan struct{})
	scope := providerTestScope(database, databasePath)
	acquireOperation := func(ctx context.Context) (func(), error) {
		close(acquireStarted)
		select {
		case <-allowAcquire:
			return func() {}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, acquireOperation))
	request := httptest.NewRequest(http.MethodGet, "/api/backup/providers/1/records/1/download", nil)
	request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	request.SetPathValue("record_id", strconv.FormatInt(record.ID, 10))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handlers.DownloadProviderRecord(response, request)
	}()

	waitForProviderTestSignal(t, acquireStarted, "download operation acquisition")
	if _, err := NewStore(database).UpdateProvider(context.Background(), provider.ID, UpdateProviderRequest{
		Name: provider.Name, Status: "disabled", Public: provider.Public,
	}); err != nil {
		t.Fatal(err)
	}
	close(allowAcquire)
	waitForProviderTestSignal(t, done, "queued download completion")

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "provider is disabled") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if calls := remoteCalls.Load(); calls != 0 {
		t.Fatalf("remote service received %d calls after provider was disabled", calls)
	}
}

func TestEnableProviderRollsBackWhenRequiredAuditFails(t *testing.T) {
	service := newProviderInfoTestServer(t)
	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "disabled")
	wantErr := errors.New("required audit unavailable")
	scope := providerTestScope(database, databasePath)
	scope.Mutate = transactionRunner(database, wantErr)

	if _, err := EnableProvider(context.Background(), scope, provider.ID); !errors.Is(err, wantErr) {
		t.Fatalf("enable error = %v, want %v", err, wantErr)
	}
	stored, err := NewStore(database).GetProvider(context.Background(), provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "disabled" || stored.LastCheckedAt != nil {
		t.Fatalf("provider mutation escaped failed audit: %#v", stored)
	}
}

func TestPreparedProviderRestoreOwnsBaselineAndTemporaryFileCleanup(t *testing.T) {
	data := []byte("encrypted remote database")
	digest := sha256.Sum256(data)
	checksum := hex.EncodeToString(digest[:])
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="database.aipdb"`)
		w.Header().Set("X-AIPermission-Backup-ID", "backup-restore")
		w.Header().Set("X-AIPermission-SHA256", checksum)
		_, _ = w.Write(data)
	}))
	t.Cleanup(service.Close)
	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
	record, err := NewStore(database).UpsertRecord(context.Background(), CreateRecordRequest{
		ProviderID: provider.ID, DatabaseID: "db-test", DatabaseName: "Test Database",
		ProviderFileID: "backup-restore", Filename: "database.aipdb", SizeBytes: int64(len(data)),
		ChecksumSHA256: checksum, BackupCreatedAt: "2026-09-10T10:00:00Z", UploadedAt: "2026-09-10T10:00:00Z",
		Metadata: map[string]any{"stream_id": "workspace-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareProviderRestore(context.Background(), providerTestScope(database, databasePath), provider.ID, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.Path); err != nil {
		t.Fatalf("prepared file: %v", err)
	}
	restoredDatabase, _ := openProviderTestDatabase(t)
	if err := prepared.RecordBaseline(context.Background(), restoredDatabase); err != nil {
		t.Fatalf("record baseline: %v", err)
	}
	baseline, err := ReadServiceBaseline(context.Background(), restoredDatabase, service.URL, "workspace-test")
	if err != nil || baseline == nil || baseline.BackupID != record.ProviderFileID {
		t.Fatalf("baseline = %#v, err = %v", baseline, err)
	}
	prepared.Remove()
	prepared.Remove()
	if _, err := os.Stat(prepared.Path); !os.IsNotExist(err) {
		t.Fatalf("prepared file was not removed: %v", err)
	}
}

func openProviderTestDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "provider.aipdb")
	database, err := dbpkg.OpenEncrypted(path, "StrongDatabasePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database, path
}

func createProviderTestRecord(t *testing.T, database *sql.DB, baseURL, token, status string) Provider {
	t.Helper()
	provider, err := NewStore(database).CreateProvider(context.Background(), CreateProviderRequest{
		ProviderType: ServiceProviderType, Name: "Backup", Status: status,
		Public:    map[string]any{"base_url": baseURL, "stream_id": "workspace-test", "database_name": "Test Database"},
		Encrypted: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func providerTestScope(database *sql.DB, databasePath string) HTTPScope {
	return HTTPScope{
		Database: database, DatabaseID: "db-test", DatabaseName: "Test Database",
		DatabasePath: databasePath, WorkspaceUUID: "workspace-test", InstallationDataPath: filepath.Dir(databasePath),
		Secrets:       testProviderSecretCodec{},
		Mutate:        transactionRunner(database, nil),
		AuditRequired: func(context.Context, string, any) error { return nil },
		Observe:       func(context.Context, string, any) {},
	}
}

func providerTestOperationScope(scope HTTPScope, acquire func(context.Context) (func(), error)) OperationHTTPScopeProvider {
	if acquire == nil {
		acquire = func(context.Context) (func(), error) { return func() {}, nil }
	}
	return func(w http.ResponseWriter, r *http.Request) (HTTPScope, func(), bool) {
		release, err := acquire(r.Context())
		if err != nil {
			httptransport.WriteError(w, http.StatusRequestTimeout, "backup operation was canceled")
			return HTTPScope{}, nil, false
		}
		return scope, release, true
	}
}

func transactionRunner(database *sql.DB, requiredAuditErr error) auditedmutation.Runner {
	return func(ctx context.Context, _ string, payload func() any, mutate func(*sql.Tx) error) error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := mutate(tx); err != nil {
			return err
		}
		_ = payload()
		if requiredAuditErr != nil {
			return requiredAuditErr
		}
		return tx.Commit()
	}
}

func newProviderInfoTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ServiceInfo{
			Service: "aipermission-backup", Version: "test", ProtocolVersion: ServiceProtocol,
			Capabilities: append([]string(nil), requiredServiceCapabilities...), MaxUploadBytes: MaxDatabaseTransferBytes, StorageSchema: 1,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func waitForProviderTestSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}
