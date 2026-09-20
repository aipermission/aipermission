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

type observingProviderSecretCodec struct {
	decrypted chan string
}

func writeCompatibleServiceInfo(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(ServiceInfo{
		Service: "aipermission-backup", ProtocolVersion: ServiceProtocol,
		Capabilities: append([]string(nil), requiredServiceCapabilities...),
	}); err != nil {
		t.Errorf("encode backup service info: %v", err)
	}
}

func (testProviderSecretCodec) EncryptProviderSecret(_ int64, secret map[string]any) (string, error) {
	token, _ := secret["token"].(string)
	return token, nil
}

func (testProviderSecretCodec) DecryptProviderSecret(provider Provider) (map[string]any, error) {
	return map[string]any{"token": provider.EncryptedSecretJSON}, nil
}

func (codec observingProviderSecretCodec) EncryptProviderSecret(_ int64, secret map[string]any) (string, error) {
	token, _ := secret["token"].(string)
	return token, nil
}

func (codec observingProviderSecretCodec) DecryptProviderSecret(provider Provider) (map[string]any, error) {
	codec.decrypted <- provider.EncryptedSecretJSON
	return map[string]any{"token": provider.EncryptedSecretJSON}, nil
}

func TestProviderStateIsLoadedAfterProviderOperationLease(t *testing.T) {
	var receivedAuthorization string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			writeCompatibleServiceInfo(t, w)
			return
		}
		receivedAuthorization = r.Header.Get("Authorization")
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upload: %v", err)
			return
		}
		digest := sha256.Sum256(data)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(ServiceBackup{
			ID: "backup-current-provider", StreamID: "workspace-test", DatabaseName: "Test Database",
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
	decrypted := make(chan string, 2)
	scope := providerTestScope(database, databasePath)
	scope.Secrets = observingProviderSecretCodec{decrypted: decrypted}
	scope.CreateSnapshot = func(context.Context) (DatabaseSnapshot, error) {
		return DatabaseSnapshot{Path: snapshotPath}, nil
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))
	release, err := handlers.providerOps.Acquire(t.Context(), database, provider.ID)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", strings.NewReader(`{"idempotency_key":"leased-provider"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	response := httptest.NewRecorder()
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		defer close(done)
		handlers.UploadProviderBackup(response, request)
	}()
	<-started
	select {
	case token := <-decrypted:
		release()
		t.Fatalf("provider credential %q was decrypted before acquiring its operation lease", token)
	case <-time.After(100 * time.Millisecond):
	}
	rotated := testNewServiceToken
	if _, err := NewStore(database).UpdateProvider(context.Background(), provider.ID, UpdateProviderRequest{
		Name: provider.Name, Status: provider.Status, Public: provider.Public, Encrypted: &rotated,
	}); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	waitForProviderTestSignal(t, done, "leased provider upload")

	if response.Code != http.StatusCreated {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if receivedAuthorization != "Bearer "+testNewServiceToken {
		t.Fatalf("authorization = %q, want rotated credential", receivedAuthorization)
	}
}

func TestCanceledProviderLeaseReturnsExplicitHTTPError(t *testing.T) {
	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, "http://127.0.0.1:1", testOldServiceToken, "active")
	scope := providerTestScope(database, databasePath)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))
	release, err := handlers.providerOps.Acquire(t.Context(), database, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/test", nil).WithContext(ctx)
	request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	response := httptest.NewRecorder()
	handlers.TestProvider(response, request)
	if response.Code != http.StatusRequestTimeout || !strings.Contains(response.Body.String(), "operation was canceled") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestQueuedUploadReloadsRotatedProviderCredential(t *testing.T) {
	var receivedAuthorization string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			writeCompatibleServiceInfo(t, w)
			return
		}
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

func TestProviderOperationsSerializeRemoteSnapshotReconciliationWithUpload(t *testing.T) {
	payload := []byte("serialized encrypted snapshot")
	digest := sha256.Sum256(payload)
	listStarted := make(chan struct{})
	releaseList := make(chan struct{})
	uploadReached := make(chan struct{})
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/streams/workspace-test/backups":
			close(listStarted)
			<-releaseList
			_ = json.NewEncoder(w).Encode(servicePage[ServiceBackup]{Items: []ServiceBackup{}})
		case r.URL.Path == "/v1/info":
			close(uploadReached)
			writeCompatibleServiceInfo(t, w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/streams/workspace-test/backups":
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(body, payload) {
				t.Errorf("unexpected upload body: body=%q err=%v", body, err)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(ServiceBackup{
				ID: "backup-serialized", StreamID: "workspace-test", DatabaseName: "Test Database",
				SourceInstallationID: r.Header.Get("X-AIPermission-Source-Installation-ID"), Filename: "database.aipdb",
				SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(service.Close)

	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.aipdb")
	if err := os.WriteFile(snapshotPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	scope := providerTestScope(database, databasePath)
	scope.CreateSnapshot = func(context.Context) (DatabaseSnapshot, error) {
		return DatabaseSnapshot{Path: snapshotPath}, nil
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))

	listRequest := httptest.NewRequest(http.MethodGet, "/api/backup/providers/1/records", nil)
	listRequest.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	listResponse := httptest.NewRecorder()
	listDone := make(chan struct{})
	go func() {
		defer close(listDone)
		handlers.ListProviderRecords(listResponse, listRequest)
	}()
	waitForProviderTestSignal(t, listStarted, "remote list snapshot")

	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", strings.NewReader(`{"idempotency_key":"serialized-upload"}`))
	uploadRequest.Header.Set("Content-Type", "application/json")
	uploadRequest.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	uploadResponse := httptest.NewRecorder()
	uploadDone := make(chan struct{})
	go func() {
		defer close(uploadDone)
		handlers.UploadProviderBackup(uploadResponse, uploadRequest)
	}()

	select {
	case <-uploadReached:
		t.Fatal("upload passed an older provider snapshot reconciliation")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseList)
	waitForProviderTestSignal(t, listDone, "remote list reconciliation")
	waitForProviderTestSignal(t, uploadReached, "serialized upload")
	waitForProviderTestSignal(t, uploadDone, "serialized upload completion")

	if listResponse.Code != http.StatusOK || uploadResponse.Code != http.StatusCreated {
		t.Fatalf("responses: list=%d %s upload=%d %s", listResponse.Code, listResponse.Body.String(), uploadResponse.Code, uploadResponse.Body.String())
	}
	record, err := NewStore(database).GetRecordByProviderFileID(t.Context(), provider.ID, "backup-serialized")
	if err != nil || record.DeletedAt != nil {
		t.Fatalf("uploaded record was reconciled as missing: record=%#v err=%v", record, err)
	}
	operation, err := NewStore(database).GetUploadOperation(t.Context(), "serialized-upload")
	if err != nil || operation.Status != "completed" || operation.ProviderFileID != record.ProviderFileID {
		t.Fatalf("upload operation = %#v, err=%v", operation, err)
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
				if r.URL.Path == "/v1/info" {
					writeCompatibleServiceInfo(t, w)
					return
				}
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

func TestUploadRetryStopsAfterRemoteOperationTombstoneExpires(t *testing.T) {
	var remoteCalls atomic.Int64
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			writeCompatibleServiceInfo(t, w)
			return
		}
		remoteCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":{"code":"operation_expired","message":"upload operation expired"}}`))
	}))
	t.Cleanup(service.Close)

	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.aipdb")
	if err := os.WriteFile(snapshotPath, []byte("encrypted snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	scope := providerTestScope(database, databasePath)
	var snapshots atomic.Int64
	scope.CreateSnapshot = func(context.Context) (DatabaseSnapshot, error) {
		snapshots.Add(1)
		return DatabaseSnapshot{Path: snapshotPath}, nil
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))
	invoke := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", strings.NewReader(`{"idempotency_key":"expired-upload"}`))
		request.Header.Set("Content-Type", "application/json")
		request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
		response := httptest.NewRecorder()
		handlers.UploadProviderBackup(response, request)
		return response
	}

	for attempt := 0; attempt < 2; attempt++ {
		response := invoke()
		if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), `"code":"operation_expired"`) {
			t.Fatalf("attempt %d response = %d %s", attempt+1, response.Code, response.Body.String())
		}
	}
	operation, err := NewStore(database).GetUploadOperation(context.Background(), "expired-upload")
	if err != nil || operation.Status != "expired" || operation.CompletedAt == nil {
		t.Fatalf("expired operation was not terminal: operation=%#v err=%v", operation, err)
	}
	if remoteCalls.Load() != 1 || snapshots.Load() != 1 {
		t.Fatalf("remote calls=%d snapshots=%d, want one terminal reconciliation attempt", remoteCalls.Load(), snapshots.Load())
	}
}

func TestCompletedUploadReplayExpiresAfterRemoteRetention(t *testing.T) {
	var listCalls atomic.Int64
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/info":
			writeCompatibleServiceInfo(t, w)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/streams/workspace-test/backups":
			listCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[],"next_cursor":""}`))
		default:
			t.Errorf("unexpected remote request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(service.Close)

	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
	store := NewStore(database)
	sourceInstallationID := backupSourceInstallationID(filepath.Dir(databasePath))
	_, _, err := store.ClaimUploadOperation(t.Context(), ClaimUploadOperationRequest{
		IdempotencyKey: "retained-response", ProviderID: provider.ID, DatabaseID: "db-test",
		StreamID: "workspace-test", SourceInstallationID: sourceInstallationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertRecord(t.Context(), CreateRecordRequest{
		ProviderID: provider.ID, DatabaseID: "db-test", DatabaseName: "Test Database",
		ProviderFileID: "backup-retained", Filename: "database.aipdb", SizeBytes: 12,
		BackupCreatedAt: "2026-09-10T10:00:00Z", UploadedAt: "2026-09-10T10:00:00Z",
		Metadata: map[string]any{"stream_id": "workspace-test"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteUploadOperation(t.Context(), "retained-response", "backup-retained"); err != nil {
		t.Fatal(err)
	}

	scope := providerTestScope(database, databasePath)
	var snapshots atomic.Int64
	scope.CreateSnapshot = func(context.Context) (DatabaseSnapshot, error) {
		snapshots.Add(1)
		return DatabaseSnapshot{}, errors.New("completed replay must not create a snapshot")
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))
	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", strings.NewReader(`{"idempotency_key":"retained-response"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	response := httptest.NewRecorder()
	handlers.UploadProviderBackup(response, request)

	operation, readErr := store.GetUploadOperation(t.Context(), "retained-response")
	if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), `"code":"operation_expired"`) ||
		readErr != nil || operation.Status != "expired" || listCalls.Load() != 1 || snapshots.Load() != 0 {
		t.Fatalf("response=%d %s operation=%#v readErr=%v lists=%d snapshots=%d",
			response.Code, response.Body.String(), operation, readErr, listCalls.Load(), snapshots.Load())
	}
}

func TestUploadRejectsIncompatibleServiceBeforeSnapshotCreation(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/info" {
			t.Errorf("unexpected request path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ServiceInfo{
			Service: "aipermission-backup", ProtocolVersion: "3",
			Capabilities: []string{"immutable_upload", "idempotent_upload"},
		})
	}))
	t.Cleanup(service.Close)

	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
	scope := providerTestScope(database, databasePath)
	var snapshots atomic.Int64
	scope.CreateSnapshot = func(context.Context) (DatabaseSnapshot, error) {
		snapshots.Add(1)
		return DatabaseSnapshot{}, errors.New("snapshot must not be created")
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))
	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", strings.NewReader(`{"idempotency_key":"old-service"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	response := httptest.NewRecorder()
	handlers.UploadProviderBackup(response, request)

	if response.Code != http.StatusBadRequest || snapshots.Load() != 0 {
		t.Fatalf("response=%d %s snapshots=%d", response.Code, response.Body.String(), snapshots.Load())
	}
}

func TestUploadDefersStaleUncertainOperationExpiryToRemoteService(t *testing.T) {
	var uploadCalls atomic.Int64
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			writeCompatibleServiceInfo(t, w)
			return
		}
		uploadCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":{"code":"operation_expired","message":"upload operation expired"}}`))
	}))
	t.Cleanup(service.Close)
	database, databasePath := openProviderTestDatabase(t)
	provider := createProviderTestRecord(t, database, service.URL, testOldServiceToken, "active")
	store := NewStore(database)
	_, _, err := store.ClaimUploadOperation(t.Context(), ClaimUploadOperationRequest{
		IdempotencyKey: "stale-uncertain-upload", ProviderID: provider.ID, DatabaseID: "db-test",
		StreamID: "workspace-test", SourceInstallationID: backupSourceInstallationID(filepath.Dir(databasePath)),
	})
	if err != nil {
		t.Fatal(err)
	}
	staleCreatedAt := time.Now().UTC().Add(-90 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := database.Exec(`UPDATE backup_upload_operations SET status = 'outcome_unknown', created_at = ? WHERE idempotency_key = ?`, staleCreatedAt, "stale-uncertain-upload"); err != nil {
		t.Fatal(err)
	}
	scope := providerTestScope(database, databasePath)
	var snapshots atomic.Int64
	scope.CreateSnapshot = func(context.Context) (DatabaseSnapshot, error) {
		snapshots.Add(1)
		path := filepath.Join(t.TempDir(), "snapshot.aipdb")
		if err := os.WriteFile(path, []byte("encrypted snapshot"), 0o600); err != nil {
			return DatabaseSnapshot{}, err
		}
		return DatabaseSnapshot{Path: path}, nil
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return scope, true }, providerTestOperationScope(scope, nil))
	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", strings.NewReader(`{"idempotency_key":"stale-uncertain-upload"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", strconv.FormatInt(provider.ID, 10))
	response := httptest.NewRecorder()
	handlers.UploadProviderBackup(response, request)

	operation, readErr := store.GetUploadOperation(t.Context(), "stale-uncertain-upload")
	if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), `"code":"operation_expired"`) ||
		readErr != nil || operation.Status != "expired" || snapshots.Load() != 1 || uploadCalls.Load() != 1 {
		t.Fatalf("response=%d %s operation=%#v readErr=%v snapshots=%d remote=%d",
			response.Code, response.Body.String(), operation, readErr, snapshots.Load(), uploadCalls.Load())
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
