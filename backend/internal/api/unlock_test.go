package api

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func newLockedAPITestServer(t *testing.T) *Server {
	t.Helper()
	catalog := newTestConnectorCatalog(t)
	return NewLockedServer(config.Config{
		Host:           "127.0.0.1",
		Port:           "8080",
		DataPath:       filepath.Join(t.TempDir(), "aipermission.db"),
		GatewaySecret:  "gateway-secret",
		AllowedOrigins: []string{"http://localhost:3001"},
	},
		WithConnectorRegistry(catalog.connectors),
		WithConnectorAdapterRegistry(catalog.adapters),
	)
}

func TestDatabaseUnlockErrorsSeparateAuthenticationFromInitialization(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "authentication",
			err:        fmt.Errorf("%w: encrypted database validation failed", errDatabaseAuthentication),
			wantStatus: http.StatusUnauthorized,
			wantBody:   "invalid unlock password or database",
		},
		{
			name:       "initialization",
			err:        fmt.Errorf("%w: migrate encrypted records: invalid envelope", errDatabaseInitialization),
			wantStatus: http.StatusConflict,
			wantBody:   "database initialization failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeDatabaseUnlockError(response, test.err)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestDatabaseInitializationErrorIncludesLatestMigrationSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.aipdb")
	older := path + ".pre-migration-v18-20260101T000000.000000000Z.aipdb"
	latest := path + ".pre-migration-v19-20260904T010000.000000000Z.aipdb"
	if err := os.WriteFile(older, []byte("snapshot"), 0o600); err != nil {
		t.Fatalf("write old snapshot fixture: %v", err)
	}
	before := preMigrationSnapshotSet(path)
	if err := os.WriteFile(latest, []byte("snapshot"), 0o600); err != nil {
		t.Fatalf("write new snapshot fixture: %v", err)
	}
	err := databaseInitializationError(path, errors.New("rewrite failed"), before)
	if !errors.Is(err, errDatabaseInitialization) || !strings.Contains(err.Error(), latest) {
		t.Fatalf("initialization error = %v", err)
	}
}

func TestDatabaseInitializationFailureDoesNotConsumePasswordAttempts(t *testing.T) {
	limiter := newConfiguredAuthRateLimiter(1, authRateLimitLockoutFailures)
	attempt := databasePasswordAttempt{limiter: limiter, key: "unlock:test"}
	attempt.failure()
	recordDatabaseUnlockAttempt(attempt, fmt.Errorf("%w: rewrite failed", errDatabaseInitialization))
	if delay := limiter.delay(attempt.key); delay != 0 {
		t.Fatalf("initialization failure retained password backoff: %s", delay)
	}
	recordDatabaseUnlockAttempt(attempt, fmt.Errorf("%w: validation failed", errDatabaseAuthentication))
	if delay := limiter.delay(attempt.key); delay == 0 {
		t.Fatal("authentication failure did not consume password attempt")
	}
}

func TestRuntimeCloseMarksRunningConnectorActionsOutcomeUnknown(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: target.ConnectorKind,
		ActionName: "runtime_close_fixture", Source: commandRequestSourceManual, Status: connectors.ResultRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.markRunningConnectorActionsOutcomeUnknown(runtime); err != nil {
		t.Fatal(err)
	}
	finished, err := store.GetActionRequest(t.Context(), request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != connectors.ResultOutcomeUnknown || finished.Error != dbpkg.ConnectorActionOutcomeUnknownMessage {
		t.Fatalf("running request was not closed safely: %#v", finished)
	}
	var auditCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE action = 'connector_action.request.outcome_unknown'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("outcome_unknown audit events=%d, want 1", auditCount)
	}
}

func TestUnlockSetupLockUnlockAndDatabaseLifecycle(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	if response := performJSON(handler, http.MethodGet, "/api/unlock/status", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"setup_required"`) {
		t.Fatalf("unlock status failed: %d %s", response.Code, response.Body.String())
	} else if strings.Contains(response.Body.String(), "data_path") || strings.Contains(response.Body.String(), server.config.DataPath) {
		t.Fatalf("locked unlock status should not expose local database paths: %s", response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{Password: "short", ConfirmPassword: "short"}); response.Code != http.StatusBadRequest {
		t.Fatalf("short setup password should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{Password: "lowercasepassword", ConfirmPassword: "lowercasepassword"}); response.Code != http.StatusBadRequest {
		t.Fatalf("weak setup password should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{Password: "LongPassword123", ConfirmPassword: "other-password"}); response.Code != http.StatusBadRequest {
		t.Fatalf("mismatched setup password should fail, got %d", response.Code)
	}

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        "LongPassword123",
		ConfirmPassword: "LongPassword123",
		DatabaseName:    "Project One",
	})
	if setup.Code != http.StatusOK || !strings.Contains(setup.Body.String(), `"unlocked"`) {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/unlock/status", "", nil); response.Code != http.StatusOK {
		t.Fatalf("post-setup unlock status failed: %d %s", response.Code, response.Body.String())
	} else if strings.Contains(response.Body.String(), "data_path") || strings.Contains(response.Body.String(), server.activeDataPath) || strings.Contains(response.Body.String(), `"path"`) {
		t.Fatalf("unlock status should omit local database paths: %s", response.Body.String())
	}
	if !server.isUnlocked() {
		t.Fatalf("server should be unlocked after setup")
	}
	runtime := server.activeRuntime()
	connectorStore := connectortargets.NewStore(runtime.database)
	target, profile := createAPITestPostgresTargetProfile(t, connectorStore, runtime.vault, runtime.workspaceUUID)
	if _, err := connectorStore.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: target.ConnectorKind,
		ActionName: "retention_fixture", Source: commandRequestSourceManual, Status: connectors.ResultCompleted,
		Input: map[string]any{"body": "retained-correspondence-fixture"}, Preview: map[string]any{"subject": "retained approval preview"},
	}); err != nil {
		t.Fatalf("insert retained connector action fixture: %v", err)
	}

	if response := performJSON(handler, http.MethodPost, "/api/databases/switch", "", switchDatabaseRequest{DatabaseID: "project-one"}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"current"`) {
		t.Fatalf("switching to current database failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/rename", "", renameDatabaseRequest{DatabaseName: "Project One"}); response.Code != http.StatusBadRequest {
		t.Fatalf("renaming to the current database name should fail, got %d", response.Code)
	}
	if !server.isUnlocked() {
		t.Fatalf("failed rename validation should not lock the active database")
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/delete", "", deleteDatabaseRequest{ConfirmName: "wrong", CurrentPassword: "LongPassword123"}); response.Code != http.StatusBadRequest {
		t.Fatalf("delete with wrong confirmation should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/delete", "", deleteDatabaseRequest{ConfirmName: "project one"}); response.Code != http.StatusBadRequest {
		t.Fatalf("delete without current password should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/delete", "", deleteDatabaseRequest{ConfirmName: "project one", CurrentPassword: "wrong-password"}); response.Code != http.StatusUnauthorized {
		t.Fatalf("delete with wrong current password should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/change-password", "", changeDatabasePasswordRequest{
		CurrentPassword: "wrong-password",
		NewPassword:     "ChangedPassword123",
		ConfirmPassword: "ChangedPassword123",
	}); response.Code != http.StatusUnauthorized {
		t.Fatalf("change password with wrong current password should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/change-password", "", changeDatabasePasswordRequest{
		CurrentPassword: "LongPassword123",
		NewPassword:     "ChangedPassword123",
		ConfirmPassword: "ChangedPassword123",
	}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"password_changed"`) {
		t.Fatalf("change password failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/rename", "", renameDatabaseRequest{
		DatabaseName:    "Renamed Database",
		CurrentPassword: "wrong-password",
	}); response.Code != http.StatusUnauthorized {
		t.Fatalf("rename with wrong current password should fail, got %d %s", response.Code, response.Body.String())
	}
	if !server.isUnlocked() {
		t.Fatalf("failed rename authentication should not lock the active database")
	}

	download := performJSON(handler, http.MethodGet, "/api/backup/download", "", nil)
	if download.Code != http.StatusOK || download.Body.Len() == 0 {
		t.Fatalf("download database failed: %d", download.Code)
	}
	downloadedPath := filepath.Join(t.TempDir(), "downloaded.aipdb")
	if err := os.WriteFile(downloadedPath, download.Body.Bytes(), 0o600); err != nil {
		t.Fatalf("write downloaded backup: %v", err)
	}
	if err := dbpkg.ValidateEncrypted(downloadedPath, "ChangedPassword123"); err != nil {
		t.Fatalf("downloaded backup should be a valid encrypted snapshot: %v", err)
	}
	downloadedDB, err := dbpkg.OpenEncrypted(downloadedPath, "ChangedPassword123")
	if err != nil {
		t.Fatalf("open downloaded backup: %v", err)
	}
	defer downloadedDB.Close()
	var retainedInput, retainedPreview string
	if err := downloadedDB.QueryRow(`SELECT input_json, preview_json FROM connector_action_requests WHERE action_name = 'retention_fixture'`).Scan(&retainedInput, &retainedPreview); err != nil {
		t.Fatalf("read retained connector action from backup: %v", err)
	}
	if !strings.Contains(retainedInput, "retained-correspondence-fixture") || !strings.Contains(retainedPreview, "retained approval preview") {
		t.Fatalf("downloaded backup lost retained action content: input=%s preview=%s", retainedInput, retainedPreview)
	}
	if response := performJSON(handler, http.MethodPost, "/api/backup/export", "", map[string]any{"passphrase": "backup-passphrase"}); response.Code != http.StatusNotFound {
		t.Fatalf("removed export endpoint should not be registered, got %d %s", response.Code, response.Body.String())
	}

	if response := performJSON(handler, http.MethodPost, "/api/databases/rename", "", renameDatabaseRequest{DatabaseName: "Renamed Database", CurrentPassword: "ChangedPassword123"}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"locked"`) {
		t.Fatalf("rename database failed: %d %s", response.Code, response.Body.String())
	}
	if server.isUnlocked() {
		t.Fatalf("renaming should lock the active database")
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{DatabaseID: "renamed-database", Password: "LongPassword123"}); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong unlock password should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{DatabaseID: "renamed-database", Password: "ChangedPassword123"}); response.Code != http.StatusOK {
		t.Fatalf("unlock renamed database failed: %d %s", response.Code, response.Body.String())
	}

	if response := performJSON(handler, http.MethodPost, "/api/lock", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"locked"`) {
		t.Fatalf("lock failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{DatabaseID: "renamed-database", Password: "ChangedPassword123"}); response.Code != http.StatusOK {
		t.Fatalf("unlock after lock failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/delete", "", deleteDatabaseRequest{ConfirmName: "renamed database", CurrentPassword: "ChangedPassword123"}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"deleted"`) {
		t.Fatalf("delete database failed: %d %s", response.Code, response.Body.String())
	}
}

func TestUnlockStatusSurfacesDatabaseCatalogRecoveryFailure(t *testing.T) {
	server := newLockedAPITestServer(t)
	defer server.Close()

	quarantine := filepath.Join(filepath.Dir(server.config.DataPath), ".aipermission-delete-interrupted")
	if err := os.MkdirAll(filepath.Join(quarantine, "unexpected-directory"), 0o700); err != nil {
		t.Fatal(err)
	}

	response := performJSON(server.Handler(), http.MethodGet, "/api/unlock/status", "", nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("catalog recovery failure status = %d, want 500: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "setup_required") {
		t.Fatalf("catalog recovery failure was disguised as setup_required: %s", response.Body.String())
	}
}

func TestRenameMoveFailureReopensActiveDatabase(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        "ProjectPassword123",
		ConfirmPassword: "ProjectPassword123",
		DatabaseName:    "Project One",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}
	oldPath := server.activeDataPath
	server.databaseMove = func(string, string) error { return errors.New("injected move failure") }

	response := performJSON(handler, http.MethodPost, "/api/databases/rename", "", renameDatabaseRequest{
		DatabaseName:    "Renamed Project",
		CurrentPassword: "ProjectPassword123",
	})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("injected rename failure should return 500, got %d %s", response.Code, response.Body.String())
	}
	runtime := server.activeRuntime()
	if runtime == nil || runtime.id != "project-one" || runtime.path != oldPath {
		t.Fatalf("failed rename should restore the original runtime, got %#v", runtime)
	}
	if err := runtime.database.PingContext(t.Context()); err != nil {
		t.Fatalf("restored runtime should remain queryable: %v", err)
	}
	if response := performJSON(handler, http.MethodGet, "/api/unlock/status", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"unlocked"`) {
		t.Fatalf("failed rename should preserve the UI session, got %d %s", response.Code, response.Body.String())
	}
	if _, renamedPath, err := dbpkg.RenameDatabaseTarget(server.config.DataPath, oldPath, "Renamed Project"); err != nil {
		t.Fatalf("resolve renamed path: %v", err)
	} else if dbpkg.Exists(renamedPath) {
		t.Fatalf("failed rename should not leave the target database path")
	}
}

func TestUnlockPasswordsPreserveWhitespace(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	password := " LongPassword123 "
	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        password,
		ConfirmPassword: password,
		DatabaseName:    "Whitespace Password",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup with whitespace password failed: %d %s", setup.Code, setup.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/lock", "", nil); response.Code != http.StatusOK {
		t.Fatalf("lock failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{DatabaseID: "whitespace-password", Password: strings.TrimSpace(password)}); response.Code != http.StatusUnauthorized {
		t.Fatalf("trimmed password should not unlock, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{DatabaseID: "whitespace-password", Password: password}); response.Code != http.StatusOK {
		t.Fatalf("exact whitespace password should unlock: %d %s", response.Code, response.Body.String())
	}
}

func TestUnlockRejectsMissingDatabaseAndBadIDs(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	if (errPasswordMismatch{}).Error() != "password confirmation does not match" {
		t.Fatalf("unexpected password mismatch message")
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{Password: ""}); response.Code != http.StatusBadRequest {
		t.Fatalf("missing password should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{DatabaseID: "../bad", Password: "LongPassword123"}); response.Code != http.StatusBadRequest {
		t.Fatalf("bad database id should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{DatabaseID: "missing", Password: "LongPassword123"}); response.Code != http.StatusNotFound {
		t.Fatalf("missing database should fail, got %d", response.Code)
	}
}

func TestUnlockReportsUnsupportedPre02Database(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	id, path, err := dbpkg.NewDatabasePath(server.config.DataPath, "Legacy Database")
	if err != nil {
		t.Fatalf("database path: %v", err)
	}
	database, err := dbpkg.OpenEncrypted(path, "LegacyPassword123")
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	if _, err := database.Exec(`UPDATE schema_migrations SET description = 'initial schema' WHERE version = 1`); err != nil {
		t.Fatalf("mark database as legacy: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	response := performJSON(handler, http.MethodPost, "/api/unlock", "", unlockRequest{
		DatabaseID: id,
		Password:   "LegacyPassword123",
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("unsupported database should return conflict, got %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "unsupported pre-0.2") {
		t.Fatalf("expected unsupported database message, got %s", response.Body.String())
	}
}

func TestDeleteLockedDatabaseRequiresPassword(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	id, path, err := dbpkg.NewDatabasePath(server.config.DataPath, "Old Preview")
	if err != nil {
		t.Fatalf("database path: %v", err)
	}
	database, err := dbpkg.OpenEncryptedForMigration(path, "OldPassword123")
	if err != nil {
		t.Fatalf("create old database: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE legacy_marker (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("write old database: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close old database: %v", err)
	}

	if response := performJSON(handler, http.MethodPost, "/api/databases/delete-locked", "", deleteLockedDatabaseRequest{DatabaseID: id, CurrentPassword: "wrong"}); response.Code != http.StatusUnauthorized {
		t.Fatalf("delete locked database with wrong password should fail, got %d %s", response.Code, response.Body.String())
	}
	limitKey := databasePasswordRateLimitScope + ":127.0.0.1"
	server.authLimiter.mu.Lock()
	failures := server.authLimiter.entries[limitKey].failures
	server.authLimiter.mu.Unlock()
	if failures != 1 {
		t.Fatalf("wrong password recorded %d shared failures, want 1", failures)
	}
	if !dbpkg.Exists(path) {
		t.Fatalf("database should remain after failed delete")
	}
	if response := performJSON(handler, http.MethodPost, "/api/databases/delete-locked", "", deleteLockedDatabaseRequest{DatabaseID: id, CurrentPassword: "OldPassword123"}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"deleted"`) {
		t.Fatalf("delete locked database failed: %d %s", response.Code, response.Body.String())
	}
	server.authLimiter.mu.Lock()
	_, limited := server.authLimiter.entries[limitKey]
	server.authLimiter.mu.Unlock()
	if limited {
		t.Fatal("successful password verification did not clear shared backoff")
	}
	if dbpkg.Exists(path) {
		t.Fatalf("database file should be removed after locked delete")
	}
}

func TestSwitchDatabaseRejectsMissingDatabaseWithoutCreatingIt(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        "LongPassword123",
		ConfirmPassword: "LongPassword123",
		DatabaseName:    "Project One",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}

	missingPath, err := dbpkg.DatabasePath(server.config.DataPath, "missing-project")
	if err != nil {
		t.Fatalf("database path: %v", err)
	}
	response := performJSON(handler, http.MethodPost, "/api/databases/switch", "", switchDatabaseRequest{
		DatabaseID: "missing-project",
		Password:   "LongPassword123",
	})
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing switch target should fail, got %d %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(missingPath); !os.IsNotExist(err) {
		t.Fatalf("switch should not create missing database, stat err=%v", err)
	}
}

func TestDatabaseRenameAndSwitchFailuresKeepActiveRuntime(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        "ProjectOnePassword123",
		ConfirmPassword: "ProjectOnePassword123",
		DatabaseName:    "Project One",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}
	projectOne := server.activeRuntime()
	if projectOne == nil || projectOne.id != "project-one" {
		t.Fatalf("expected project-one runtime, got %#v", projectOne)
	}

	projectTwoID, projectTwoPath, err := dbpkg.NewDatabasePath(server.config.DataPath, "Project Two")
	if err != nil {
		t.Fatalf("project two path: %v", err)
	}
	projectTwoDB, err := dbpkg.OpenEncrypted(projectTwoPath, "ProjectTwoPassword123")
	if err != nil {
		t.Fatalf("create project two database: %v", err)
	}
	if err := projectTwoDB.Close(); err != nil {
		t.Fatalf("close project two database: %v", err)
	}
	if projectTwoID != "project-two" {
		t.Fatalf("unexpected project two id: %s", projectTwoID)
	}

	rename := performJSON(handler, http.MethodPost, "/api/databases/rename", "", renameDatabaseRequest{
		DatabaseName:    "Project Two",
		CurrentPassword: "ProjectOnePassword123",
	})
	if rename.Code != http.StatusBadRequest || !strings.Contains(rename.Body.String(), "database name already exists") {
		t.Fatalf("duplicate rename should fail cleanly, got %d %s", rename.Code, rename.Body.String())
	}
	if runtime := server.activeRuntime(); runtime == nil || runtime.id != "project-one" || runtime.path != projectOne.path {
		t.Fatalf("failed rename should keep project-one active, got %#v", runtime)
	}
	if !dbpkg.Exists(projectOne.path) || !dbpkg.Exists(projectTwoPath) {
		t.Fatalf("failed rename should not move database files")
	}

	switched := performJSON(handler, http.MethodPost, "/api/databases/switch", "", switchDatabaseRequest{
		DatabaseID: projectTwoID,
		Password:   "WrongPassword123",
	})
	if switched.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password switch should fail, got %d %s", switched.Code, switched.Body.String())
	}
	if runtime := server.activeRuntime(); runtime == nil || runtime.id != "project-one" || runtime.path != projectOne.path {
		t.Fatalf("failed switch should keep project-one active, got %#v", runtime)
	}

	oldCookie := currentTestUICookie()
	switched = performJSON(handler, http.MethodPost, "/api/databases/switch", "", switchDatabaseRequest{
		DatabaseID: projectTwoID,
		Password:   "ProjectTwoPassword123",
	})
	if switched.Code != http.StatusOK || !strings.Contains(switched.Body.String(), `"project-two"`) {
		t.Fatalf("valid switch should succeed, got %d %s", switched.Code, switched.Body.String())
	}
	if oldCookie != nil {
		request := httptest.NewRequest(http.MethodGet, "/api/connector-targets", nil)
		request.RemoteAddr = "127.0.0.1:12345"
		request.Host = "localhost"
		request.AddCookie(oldCookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("old ui session should not authenticate after workspace switch, got %d", response.Code)
		}
	}
	if response := performJSON(handler, http.MethodGet, "/api/connector-targets", "", nil); response.Code != http.StatusOK {
		t.Fatalf("new ui session should authenticate after workspace switch, got %d %s", response.Code, response.Body.String())
	}
}

func TestDatabaseImportRequiresMultipart(t *testing.T) {
	server := newLockedAPITestServer(t)
	defer server.Close()
	response := performJSON(server.Handler(), http.MethodPost, "/api/backup/import", "", importDatabaseRequest{
		DatabaseName:     "JSON Import",
		DatabasePassword: "ImportPassword123",
	})
	if response.Code != http.StatusUnsupportedMediaType || !strings.Contains(response.Body.String(), "multipart/form-data") {
		t.Fatalf("json import should be rejected, got %d %s", response.Code, response.Body.String())
	}
}

func TestMultipartDatabaseImportStreamsUploadedFile(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.aipdb")
	sourceDB, err := dbpkg.OpenEncrypted(sourcePath, "import-password")
	if err != nil {
		t.Fatalf("create encrypted source db: %v", err)
	}
	if _, err := sourceDB.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('gateway_secret', 'source-secret', datetime('now'))`); err != nil {
		t.Fatalf("insert source gateway secret: %v", err)
	}
	if _, err := sourceDB.Exec(`ALTER TABLE connector_action_requests DROP COLUMN retry_policy_json`); err != nil {
		t.Fatalf("remove request retry policy from import fixture: %v", err)
	}
	if _, err := sourceDB.Exec(`ALTER TABLE history_entries DROP COLUMN retry_policy_json`); err != nil {
		t.Fatalf("remove history retry policy from import fixture: %v", err)
	}
	if _, err := sourceDB.Exec(`DELETE FROM schema_migrations WHERE version = 19`); err != nil {
		t.Fatalf("downgrade import fixture to schema 18: %v", err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close source db: %v", err)
	}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source db: %v", err)
	}

	server := newLockedAPITestServer(t)
	defer server.Close()
	_, importedTargetPath, err := dbpkg.NewDatabasePathExact(server.config.DataPath, "Imported Project")
	if err != nil {
		t.Fatalf("resolve import staging path: %v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(importedTargetPath+".import"+suffix, []byte("stale import sidecar"), 0o600); err != nil {
			t.Fatalf("seed stale import sidecar %s: %v", suffix, err)
		}
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("database_name", "Imported Project"); err != nil {
		t.Fatalf("write database_name field: %v", err)
	}
	if err := writer.WriteField("database_password", "import-password"); err != nil {
		t.Fatalf("write database_password field: %v", err)
	}
	part, err := writer.CreateFormFile("sqlite", "source.aipdb")
	if err != nil {
		t.Fatalf("create sqlite part: %v", err)
	}
	if _, err := part.Write(sourceBytes); err != nil {
		t.Fatalf("write sqlite part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", body)
	request.Host = "localhost:8080"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"imported"`) {
		t.Fatalf("multipart import failed: %d %s", response.Code, response.Body.String())
	}
	if !server.isUnlocked() {
		t.Fatalf("server should be unlocked after multipart import")
	}
	importedPath, err := dbpkg.DatabasePath(server.config.DataPath, "imported-project")
	if err != nil {
		t.Fatalf("resolve imported database path: %v", err)
	}
	snapshots, err := filepath.Glob(importedPath + ".import.pre-migration-*.aipdb")
	if err != nil {
		t.Fatalf("list disposable import snapshots: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("successful import left %d disposable migration snapshot(s)", len(snapshots))
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(importedPath + ".import" + suffix); !os.IsNotExist(err) {
			t.Fatalf("successful import left stale staging sidecar %s: %v", suffix, err)
		}
	}
}

func TestImportedDatabaseOpenFailureRestoresPreviousWorkspace(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        "ProjectPassword123",
		ConfirmPassword: "ProjectPassword123",
		DatabaseName:    "Project One",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}
	previousRuntime := server.activeRuntime()

	sourcePath := filepath.Join(t.TempDir(), "source.aipdb")
	sourceDB, err := dbpkg.OpenEncrypted(sourcePath, "ImportPassword123")
	if err != nil {
		t.Fatalf("create encrypted source db: %v", err)
	}
	if _, err := sourceDB.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('gateway_secret', 'source-secret', datetime('now'))`); err != nil {
		t.Fatalf("insert source gateway secret: %v", err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close source db: %v", err)
	}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source db: %v", err)
	}
	server.runtimeOpen = func(path string, id string, password string) (*databaseRuntime, error) {
		if id == "imported-project" {
			return nil, errors.New("injected runtime open failure")
		}
		return server.openRuntime(path, id, password)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", nil)
	backupHandlers{server}.installImportedDatabase(response, request, "Imported Project", "ImportPassword123", func(path string) error {
		return os.WriteFile(path, sourceBytes, 0o600)
	})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("injected import open failure should return 500, got %d %s", response.Code, response.Body.String())
	}
	if runtime := server.activeRuntime(); runtime != previousRuntime || runtime.id != "project-one" {
		t.Fatalf("failed import should restore the previous workspace, got %#v", runtime)
	}
	if err := previousRuntime.database.PingContext(t.Context()); err != nil {
		t.Fatalf("previous workspace should remain queryable: %v", err)
	}
	importedPath, err := dbpkg.DatabasePath(server.config.DataPath, "imported-project")
	if err != nil {
		t.Fatalf("resolve imported database path: %v", err)
	}
	if dbpkg.Exists(importedPath) {
		t.Fatalf("failed import should remove the unusable installed copy")
	}
}

func TestPlaintextDatabaseImportIsRejected(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.sqlite")
	sourceDB, err := sql.Open("sqlite3", sourcePath)
	if err != nil {
		t.Fatalf("create plaintext source db: %v", err)
	}
	if _, err := sourceDB.Exec(`CREATE TABLE example (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("write plaintext source db: %v", err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close plaintext source db: %v", err)
	}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read plaintext source db: %v", err)
	}

	server := newLockedAPITestServer(t)
	defer server.Close()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("database_name", "Plain Import"); err != nil {
		t.Fatalf("write database_name field: %v", err)
	}
	if err := writer.WriteField("database_password", "PlainImportPassword123"); err != nil {
		t.Fatalf("write database_password field: %v", err)
	}
	part, err := writer.CreateFormFile("sqlite", "source.sqlite")
	if err != nil {
		t.Fatalf("create sqlite part: %v", err)
	}
	if _, err := part.Write(sourceBytes); err != nil {
		t.Fatalf("write sqlite part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", body)
	request.Host = "localhost:8080"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "plaintext SQLite imports are not supported") {
		t.Fatalf("plaintext import should be rejected, got %d %s", response.Code, response.Body.String())
	}
	importFiles, err := filepath.Glob(filepath.Join(filepath.Dir(server.config.DataPath), "*.import"))
	if err != nil {
		t.Fatalf("glob import files: %v", err)
	}
	if len(importFiles) != 0 {
		t.Fatalf("plaintext import left temp files: %v", importFiles)
	}
}

func TestRemovedRestoreEndpointIsNotRegistered(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        "LongPassword123",
		ConfirmPassword: "LongPassword123",
		DatabaseName:    "Project One",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}

	response := performJSON(handler, http.MethodPost, "/api/backup/restore", "", map[string]any{"backup": map[string]any{}})
	if response.Code != http.StatusNotFound {
		t.Fatalf("removed restore endpoint should not be registered, got %d %s", response.Code, response.Body.String())
	}
}

func TestLockPromotesRemainingUnlockedWorkspaceForMCP(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        "ProjectOnePassword123",
		ConfirmPassword: "ProjectOnePassword123",
		DatabaseName:    "Project One",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}
	runtime := server.activeRuntime()
	target := createTestSSHConnectorProfile(t, runtime.database, testSSHKeyStore(t, runtime), "worker-1")
	token, err := runtime.tokens.Create(t.Context(), tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if err := connectortargets.NewStore(runtime.database).SetActionPermission(t.Context(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.TargetID,
		ProfileID:     target.ProfileID,
		ActionName:    sshconnector.ActionExec,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("grant permission: %v", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "source.aipdb")
	sourceDB, err := dbpkg.OpenEncrypted(sourcePath, "import-password")
	if err != nil {
		t.Fatalf("create encrypted source db: %v", err)
	}
	if _, err := sourceDB.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('gateway_secret', 'source-secret', datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`); err != nil {
		t.Fatalf("insert source gateway secret: %v", err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close source db: %v", err)
	}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source db: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("database_name", "Imported Project"); err != nil {
		t.Fatalf("write database_name field: %v", err)
	}
	if err := writer.WriteField("database_password", "import-password"); err != nil {
		t.Fatalf("write database_password field: %v", err)
	}
	part, err := writer.CreateFormFile("sqlite", "source.aipdb")
	if err != nil {
		t.Fatalf("create sqlite part: %v", err)
	}
	if _, err := part.Write(sourceBytes); err != nil {
		t.Fatalf("write sqlite part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
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
	importResponse := httptest.NewRecorder()
	handler.ServeHTTP(importResponse, request)
	recordTestUICookies(importResponse.Result().Cookies())
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import failed: %d %s", importResponse.Code, importResponse.Body.String())
	}

	switchResponse := performJSON(handler, http.MethodPost, "/api/databases/switch", "", switchDatabaseRequest{DatabaseID: "project-one"})
	if switchResponse.Code != http.StatusOK {
		t.Fatalf("switch back to project one failed: %d %s", switchResponse.Code, switchResponse.Body.String())
	}

	if response := performJSON(handler, http.MethodPost, "/api/databases/switch", "", switchDatabaseRequest{DatabaseID: "imported-project"}); response.Code != http.StatusOK {
		t.Fatalf("switch to imported project failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/lock", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"project-one"`) {
		t.Fatalf("lock should promote remaining unlocked workspace, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"target_name":"worker-1"`) {
		t.Fatalf("MCP connector targets should keep working for remaining unlocked workspace, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/lock", "", map[string]string{"scope": "all"}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"locked"`) {
		t.Fatalf("lock all should lock every workspace, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil); response.Code != http.StatusLocked {
		t.Fatalf("MCP should stop after lock all, got %d %s", response.Code, response.Body.String())
	}
}

func TestLockMarksRunningCommandRequestsAsError(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	password := "ProjectOnePassword123"
	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        password,
		ConfirmPassword: password,
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}

	runtime := server.activeRuntime()
	target := createTestSSHConnectorProfile(t, runtime.database, testSSHKeyStore(t, runtime), "worker-1")
	token, err := runtime.tokens.Create(t.Context(), tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	requestID, err := server.insertCommandRequest(t.Context(), runtime, token.ID, target.ID, "sleep 60", "test lock cleanup", "running")
	if err != nil {
		t.Fatalf("insert running request: %v", err)
	}

	if response := performJSON(handler, http.MethodPost, "/api/lock", "", map[string]string{"scope": "all"}); response.Code != http.StatusOK {
		t.Fatalf("lock all failed: %d %s", response.Code, response.Body.String())
	}

	reopened, err := dbpkg.OpenEncrypted(server.config.DataPath, password)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer reopened.Close()
	var status string
	var errorText string
	if err := reopened.QueryRow(`SELECT status, error FROM command_requests WHERE id = ?`, requestID).Scan(&status, &errorText); err != nil {
		t.Fatalf("read command request: %v", err)
	}
	if status != "error" || !strings.Contains(errorText, "workspace locked") {
		t.Fatalf("running request should be marked error on lock, status=%s error=%q", status, errorText)
	}
}

func TestDeleteActiveDatabasePromotesRemainingUnlockedWorkspace(t *testing.T) {
	server := newLockedAPITestServer(t)
	handler := server.Handler()
	defer server.Close()

	setup := performJSON(handler, http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password:        "ProjectOnePassword123",
		ConfirmPassword: "ProjectOnePassword123",
		DatabaseName:    "Project One",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup failed: %d %s", setup.Code, setup.Body.String())
	}

	sourcePath := filepath.Join(t.TempDir(), "source.aipdb")
	sourceDB, err := dbpkg.OpenEncrypted(sourcePath, "import-password")
	if err != nil {
		t.Fatalf("create encrypted source db: %v", err)
	}
	if _, err := sourceDB.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('gateway_secret', 'source-secret', datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`); err != nil {
		t.Fatalf("insert source gateway secret: %v", err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close source db: %v", err)
	}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source db: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("database_name", "Imported Project"); err != nil {
		t.Fatalf("write database_name field: %v", err)
	}
	if err := writer.WriteField("database_password", "import-password"); err != nil {
		t.Fatalf("write database_password field: %v", err)
	}
	part, err := writer.CreateFormFile("sqlite", "source.aipdb")
	if err != nil {
		t.Fatalf("create sqlite part: %v", err)
	}
	if _, err := part.Write(sourceBytes); err != nil {
		t.Fatalf("write sqlite part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
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
	importResponse := httptest.NewRecorder()
	handler.ServeHTTP(importResponse, request)
	recordTestUICookies(importResponse.Result().Cookies())
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import failed: %d %s", importResponse.Code, importResponse.Body.String())
	}

	response := performJSON(handler, http.MethodPost, "/api/databases/delete", "", deleteDatabaseRequest{ConfirmName: "imported project", CurrentPassword: "import-password"})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"unlocked"`) || !strings.Contains(response.Body.String(), `"project-one"`) {
		t.Fatalf("delete should promote project-one, got %d %s", response.Code, response.Body.String())
	}
	if runtime := server.activeRuntime(); runtime == nil || runtime.id != "project-one" {
		t.Fatalf("expected project-one runtime to remain active, got %#v", runtime)
	}
}
