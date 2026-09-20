package connectormanagement

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	restored      string
	restoreCalls  int
	restoreErr    error
	restoreResult connectors.ActionResult
	cancelRestore context.CancelFunc
	backupCheck   func()
	restoreCheck  func()
}

func (*profileBackupTestConnector) Kind() string { return profileBackupTestKind }

func (c *profileBackupTestConnector) Backup(context.Context, connectors.RuntimeContext, connectors.BackupRequest) (connectors.BackupArtifact, error) {
	if c.backupCheck != nil {
		c.backupCheck()
	}
	return connectors.BackupArtifact{Filename: "database.sql", ContentType: "application/sql", Data: []byte("select 1;\n")}, nil
}

func (c *profileBackupTestConnector) Restore(_ context.Context, _ connectors.RuntimeContext, request connectors.RestoreRequest) (connectors.ActionResult, error) {
	if c.restoreCheck != nil {
		c.restoreCheck()
	}
	c.restoreCalls++
	content, err := io.ReadAll(request.Content)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	c.restored = string(content)
	if c.cancelRestore != nil {
		c.cancelRestore()
	}
	if c.restoreErr != nil {
		return connectors.ActionResult{}, c.restoreErr
	}
	if c.restoreResult.Status != "" {
		return c.restoreResult, nil
	}
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
	audited := make([]map[string]any, 0, 3)
	auditActors := make([]string, 0, 3)
	var auditErr error
	var commitReplyErr error
	var redactErr error
	exclusiveHeld := false
	exclusiveAcquires := 0
	deliveryHeld := false
	deliveryAcquires := 0
	handler := NewProfileBackupHTTPHandler(func(http.ResponseWriter) (ProfileBackupScope, bool) {
		return ProfileBackupScope{
			Database: fixture.database, Registry: registry,
			AcquireDelivery: func(context.Context) (func(), error) {
				deliveryAcquires++
				deliveryHeld = true
				return func() { deliveryHeld = false }, nil
			},
			AcquireExclusive: func(context.Context) (func(), error) {
				exclusiveAcquires++
				exclusiveHeld = true
				return func() { exclusiveHeld = false }, nil
			},
			Admission: managementTestDeliveryAdmission,
			Runtime: CredentialRuntimePorts{
				DecryptSecret: func(context.Context, int64, string) (map[string]any, error) { return map[string]any{}, nil },
				RuntimeContext: func(target connectortargets.Target, profile connectortargets.CredentialProfile, _ map[string]any, _ CredentialBoundary) connectors.RuntimeContext {
					return connectors.RuntimeContext{
						Target:  connectors.TargetView{ID: target.ID, ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config},
						Profile: connectortargets.CredentialProfileView(profile),
					}
				},
				RedactResult: func(_ context.Context, result connectors.ActionResult, _ CredentialBoundary) (connectors.ActionResult, error) {
					return result, redactErr
				},
				RedactText: func(_ context.Context, value string) string { return value },
			},
			Observe: func(_ context.Context, action string, _ map[string]any) { observed = append(observed, action) },
			WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
				tx, err := fixture.database.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				err = mutate(tx, func(_ *sql.Tx, actor string, _ *int64, _ int64, action string, payload any) error {
					if ctx.Err() != nil {
						t.Fatalf("restore audit context was canceled: %v", ctx.Err())
					}
					observed = append(observed, action)
					auditActors = append(auditActors, actor)
					audited = append(audited, payload.(map[string]any))
					return auditErr
				})
				if err != nil {
					return err
				}
				if err := tx.Commit(); err != nil {
					return err
				}
				return commitReplyErr
			},
		}, true
	})
	connector.backupCheck = func() {
		if !deliveryHeld {
			t.Fatal("backup executed without the shared workspace delivery gate")
		}
	}
	connector.restoreCheck = func() {
		if !exclusiveHeld {
			t.Fatal("restore executed without the exclusive workspace gate")
		}
	}
	mux := http.NewServeMux()
	pattern := "/targets/{id}/profiles/{profile_id}"
	mux.HandleFunc("GET "+pattern+"/backup", handler.Download)
	mux.HandleFunc("POST "+pattern+"/restore", handler.Restore)
	basePath := "/targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(profile.ID, 10)

	assertProfileBackupAndReplayFromTombstone(t, mux, basePath, target, connector, fixture.database, &observed, &audited)
	if deliveryHeld || deliveryAcquires != 1 {
		t.Fatalf("backup delivery held=%v acquires=%d", deliveryHeld, deliveryAcquires)
	}
	if len(auditActors) != 1 || auditActors[0] != "user" {
		t.Fatalf("live restore audit actors = %v", auditActors)
	}

	connector.restoreErr = connectors.ClassifyOutcomeUnknown("process_observation", nil, errors.New("connection lost"))
	body, contentType := profileRestoreBody(t, target.Name, "uncertain.sql", "restore payload")
	uncertain := httptest.NewRecorder()
	uncertainRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	uncertainRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(uncertain, uncertainRequest)
	if uncertain.Code != http.StatusConflict || len(audited) != 2 || audited[1]["status"] != string(connectors.ResultOutcomeUnknown) {
		t.Fatalf("uncertain restore = %d %s audited=%#v", uncertain.Code, uncertain.Body.String(), audited)
	}
	if !strings.Contains(uncertain.Body.String(), `"status":"outcome_unknown"`) || !strings.Contains(uncertain.Body.String(), `"operation_id":`) {
		t.Fatalf("uncertain restore response lacks reconciliation identity: %s", uncertain.Body.String())
	}
	if audited[1]["error_code"] != "outcome_unknown" {
		t.Fatalf("uncertain restore error code = %#v", audited[1])
	}

	connector.restoreErr = nil
	connector.restoreResult = connectors.OutcomeUnknownResult("response", nil, errors.New("reply lost"))
	body, contentType = profileRestoreBody(t, target.Name, "result-uncertain.sql", "restore payload")
	resultUncertain := httptest.NewRecorder()
	resultUncertainRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	resultUncertainRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(resultUncertain, resultUncertainRequest)
	if resultUncertain.Code != http.StatusConflict || len(audited) != 3 || audited[2]["status"] != string(connectors.ResultOutcomeUnknown) {
		t.Fatalf("result-form uncertain restore = %d %s audited=%#v", resultUncertain.Code, resultUncertain.Body.String(), audited)
	}
	connector.restoreResult = connectors.ActionResult{}

	requestCtx, cancel := context.WithCancel(context.Background())
	connector.restoreErr = context.Canceled
	connector.cancelRestore = cancel
	body, contentType = profileRestoreBody(t, target.Name, "canceled.sql", "restore payload")
	canceled := httptest.NewRecorder()
	canceledRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body).WithContext(requestCtx)
	canceledRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(canceled, canceledRequest)
	if len(audited) != 4 || audited[3]["status"] != "canceled" {
		t.Fatalf("canceled restore audit = %#v", audited)
	}

	connector.restoreErr = nil
	connector.cancelRestore = nil
	redactErr = errors.New("redaction unavailable")
	body, contentType = profileRestoreBody(t, target.Name, "projection-failure.sql", "restore payload")
	projectionFailure := httptest.NewRecorder()
	projectionFailureRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	projectionFailureRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(projectionFailure, projectionFailureRequest)
	if projectionFailure.Code != http.StatusConflict || !strings.Contains(projectionFailure.Body.String(), "result_projection_failed") ||
		len(audited) != 5 || audited[4]["status"] != string(connectors.ResultCompleted) || audited[4]["projection_error_code"] != "result_redaction_failed" {
		t.Fatalf("projection failure = %d %s audited=%#v", projectionFailure.Code, projectionFailure.Body.String(), audited)
	}
	redactErr = nil
	assertProfileRestoreAuditFailureRecovery(t, mux, basePath, target.Name, connector, fixture.database, &auditErr)

	auditErr = nil
	commitReplyErr = errors.New("commit reply lost")
	body, contentType = profileRestoreBody(t, target.Name, "commit-reply-lost.sql", "restore payload")
	commitLost := httptest.NewRecorder()
	commitLostRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	commitLostRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(commitLost, commitLostRequest)
	if commitLost.Code != http.StatusOK || !strings.Contains(commitLost.Body.String(), `"replayed":true`) {
		t.Fatalf("commit-reply-lost restore = %d %s", commitLost.Code, commitLost.Body.String())
	}
	var commitLostResponse struct {
		OperationID int64  `json:"operation_id"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(commitLost.Body.Bytes(), &commitLostResponse); err != nil {
		t.Fatalf("decode commit-reply-lost restore: %v", err)
	}
	if commitLostResponse.OperationID < 1 || commitLostResponse.Status != string(connectors.ResultCompleted) {
		t.Fatalf("commit-reply-lost identity = %#v", commitLostResponse)
	}
	commitReplyErr = nil
	callsAfterCommitLoss := connector.restoreCalls
	body, contentType = profileRestoreBody(t, target.Name, "commit-reply-lost.sql", "restore payload")
	commitReplay := httptest.NewRecorder()
	commitReplayRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	commitReplayRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(commitReplay, commitReplayRequest)
	if commitReplay.Code != http.StatusOK || connector.restoreCalls != callsAfterCommitLoss || !strings.Contains(commitReplay.Body.String(), `"replayed":true`) {
		t.Fatalf("commit-reply-lost replay = %d %s calls=%d", commitReplay.Code, commitReplay.Body.String(), connector.restoreCalls)
	}

	connector.restoreErr = errors.New("restore rejected")
	commitReplyErr = errors.New("commit reply lost")
	body, contentType = profileRestoreBody(t, target.Name, "failed-commit-reply-lost.sql", "restore payload")
	failedCommitLost := httptest.NewRecorder()
	failedCommitLostRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	failedCommitLostRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(failedCommitLost, failedCommitLostRequest)
	if failedCommitLost.Code != http.StatusConflict || !strings.Contains(failedCommitLost.Body.String(), `"status":"failed"`) ||
		strings.Contains(failedCommitLost.Body.String(), "audit record could not be confirmed") {
		t.Fatalf("failed commit-reply-lost restore = %d %s", failedCommitLost.Code, failedCommitLost.Body.String())
	}
	connector.restoreErr = nil
	commitReplyErr = nil
	if exclusiveAcquires == 0 || exclusiveHeld {
		t.Fatalf("exclusive restore gate acquires=%d held=%v", exclusiveAcquires, exclusiveHeld)
	}
}

func assertProfileBackupAndReplayFromTombstone(
	t *testing.T,
	mux *http.ServeMux,
	basePath string,
	target connectortargets.Target,
	connector *profileBackupTestConnector,
	database *sql.DB,
	observed *[]string,
	audited *[]map[string]any,
) {
	t.Helper()
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
	if restore.Code != http.StatusOK || connector.restored != "restore payload" || len(*observed) != 2 {
		t.Fatalf("restore = %d %s restored=%q observed=%v", restore.Code, restore.Body.String(), connector.restored, *observed)
	}
	if len(*audited) != 1 || (*audited)[0]["status"] != string(connectors.ResultCompleted) || (*audited)[0]["error_code"] != nil {
		t.Fatalf("successful restore audit = %#v", *audited)
	}
	if _, err := database.Exec(`DELETE FROM profile_restore_operations WHERE idempotency_key = ?`, "backup.sql-attempt"); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := database.QueryRow(`SELECT COUNT(*) FROM profile_restore_idempotency_tombstones WHERE idempotency_key = ?`, "backup.sql-attempt").Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("restore tombstone count=%d err=%v", retained, err)
	}
	body, contentType = profileRestoreBody(t, target.Name, "backup.sql", "restore payload")
	replay := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	replayRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(replay, replayRequest)
	if replay.Code != http.StatusOK || connector.restoreCalls != 1 || !strings.Contains(replay.Body.String(), `"replayed":true`) {
		t.Fatalf("restore replay = %d %s calls=%d", replay.Code, replay.Body.String(), connector.restoreCalls)
	}
	body, contentType = profileRestoreBodyWithKey(t, target.Name, "backup.sql", "changed payload", "backup.sql-attempt")
	drift := httptest.NewRecorder()
	driftRequest := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	driftRequest.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(drift, driftRequest)
	if drift.Code != http.StatusConflict || connector.restoreCalls != 1 || !strings.Contains(drift.Body.String(), "idempotency_conflict") {
		t.Fatalf("restore identity drift = %d %s calls=%d", drift.Code, drift.Body.String(), connector.restoreCalls)
	}
}

func assertProfileRestoreAuditFailureRecovery(
	t *testing.T,
	mux *http.ServeMux,
	basePath string,
	targetName string,
	connector *profileBackupTestConnector,
	database *sql.DB,
	auditErr *error,
) {
	t.Helper()
	*auditErr = errors.New("audit unavailable")
	body, contentType := profileRestoreBody(t, targetName, "audit-failure.sql", "restore payload")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	request.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "audit_persistence_failed") || !strings.Contains(response.Body.String(), `"status":"outcome_unknown"`) {
		t.Fatalf("audit persistence failure = %d %s", response.Code, response.Body.String())
	}
	calls := connector.restoreCalls
	body, contentType = profileRestoreBody(t, targetName, "audit-failure.sql", "restore payload")
	replay := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, basePath+"/restore", body)
	request.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(replay, request)
	if replay.Code != http.StatusConflict || connector.restoreCalls != calls || !strings.Contains(replay.Body.String(), "audit_persistence_failed") {
		t.Fatalf("audit-failed replay = %d %s calls=%d", replay.Code, replay.Body.String(), connector.restoreCalls)
	}
	var status, code string
	var pending bool
	if err := database.QueryRowContext(t.Context(), `
		SELECT status, error_code, audit_pending FROM profile_restore_operations WHERE idempotency_key = ?`,
		"audit-failure.sql-attempt").Scan(&status, &code, &pending); err != nil {
		t.Fatal(err)
	}
	if status != "outcome_unknown" || code != "audit_persistence_failed" || !pending {
		t.Fatalf("audit-failed restore state = status=%q code=%q pending=%v", status, code, pending)
	}
	*auditErr = nil
}

func TestProfileRestoreSizeValidationSeparatesArtifactFromMultipartEnvelope(t *testing.T) {
	if status, message := profileRestoreSizeError(MaxProfileRestoreBodyBytes); status != 0 || message != "" {
		t.Fatalf("exact-size artifact rejected: status=%d message=%q", status, message)
	}
	if status, _ := profileRestoreSizeError(MaxProfileRestoreBodyBytes + 1); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize artifact status = %d", status)
	}
	for _, size := range []int64{-1, 0} {
		if status, _ := profileRestoreSizeError(size); status != http.StatusBadRequest {
			t.Fatalf("empty/unknown artifact size %d status = %d", size, status)
		}
	}
	if maxProfileRestoreMultipartOverhead <= 0 {
		t.Fatal("multipart envelope must have a bounded allowance beyond the artifact limit")
	}
}

func TestProfileRestoreArtifactHashHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := profileRestoreArtifactHash(ctx, bytes.NewReader([]byte("select 1;")), int64(len("select 1;")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled artifact hash error = %v", err)
	}
}

func TestRecoverProfileRestoresCommitsTerminalAuditAtomically(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	store := connectortargets.NewStore(fixture.database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: profileBackupTestKind, Name: "Recovery database", Config: map[string]any{},
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
	result, err := fixture.database.ExecContext(t.Context(), `
		INSERT INTO profile_restore_operations (
			idempotency_key, identity_hash, target_id, profile_id, connector_kind,
			filename, artifact_sha256, size_bytes, status, created_at, updated_at
		) VALUES ('recovery-key', 'identity', ?, ?, ?, 'restore.sql', 'sha256', 9, 'running', datetime('now'), datetime('now'))`,
		target.ID, profile.ID, profileBackupTestKind)
	if err != nil {
		t.Fatal(err)
	}
	operationID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	var audited map[string]any
	auditErr := errors.New("audit unavailable")
	recover := func() error {
		return RecoverProfileRestores(t.Context(), func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
			tx, err := fixture.database.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err := mutate(tx, func(_ *sql.Tx, _ string, _ *int64, _ int64, _ string, payload any) error {
				audited = payload.(map[string]any)
				return auditErr
			}); err != nil {
				return err
			}
			return tx.Commit()
		})
	}
	if err := recover(); !errors.Is(err, auditErr) {
		t.Fatalf("recovery audit failure = %v", err)
	}
	operation, err := getProfileRestore(t.Context(), fixture.database, "recovery-key")
	if err != nil || operation.Status != "running" {
		t.Fatalf("audit failure changed restore operation: %#v err=%v", operation, err)
	}

	auditErr = nil
	if err := recover(); err != nil {
		t.Fatal(err)
	}
	operation, err = getProfileRestore(t.Context(), fixture.database, "recovery-key")
	if err != nil || operation.Status != string(connectors.ResultOutcomeUnknown) || operation.ErrorCode != "gateway_restarted" {
		t.Fatalf("recovered restore operation = %#v err=%v", operation, err)
	}
	if audited["operation_id"] != operationID || audited["status"] != string(connectors.ResultOutcomeUnknown) || audited["error_code"] != "gateway_restarted" {
		t.Fatalf("recovery audit payload = %#v", audited)
	}
	pendingResult, err := fixture.database.ExecContext(t.Context(), `
		INSERT INTO profile_restore_operations (
			idempotency_key, identity_hash, target_id, profile_id, connector_kind,
			filename, artifact_sha256, size_bytes, status, error_code, audit_pending,
			created_at, updated_at, completed_at
		) VALUES ('audit-pending-key', 'identity-2', ?, ?, ?, 'pending.sql', 'sha256', 9,
			'outcome_unknown', 'audit_persistence_failed', 1, datetime('now'), datetime('now'), datetime('now'))`,
		target.ID, profile.ID, profileBackupTestKind)
	if err != nil {
		t.Fatal(err)
	}
	pendingID, _ := pendingResult.LastInsertId()
	if err := recover(); err != nil {
		t.Fatal(err)
	}
	pending, err := getProfileRestore(t.Context(), fixture.database, "audit-pending-key")
	if err != nil || pending.AuditPending || pending.Status != "outcome_unknown" || pending.ErrorCode != "audit_persistence_failed" {
		t.Fatalf("recovered pending audit = %#v err=%v", pending, err)
	}
	if audited["operation_id"] != pendingID || audited["error_code"] != "audit_persistence_failed" {
		t.Fatalf("pending audit identity changed: %#v", audited)
	}
}

func TestProfileRestoreClaimConflictsPreserveOperationIdentity(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	store := connectortargets.NewStore(fixture.database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: profileBackupTestKind, Name: "Claim identity", Config: map[string]any{},
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
	claim := profileRestoreClaim{
		IdempotencyKey: "first-key", IdentityHash: "first-identity", TargetID: target.ID,
		ProfileID: profile.ID, ConnectorKind: profileBackupTestKind, Filename: "first.sql",
		ArtifactSHA256: "sha", SizeBytes: 3,
	}
	created, fresh, err := claimProfileRestore(t.Context(), fixture.database, claim)
	if err != nil || !fresh {
		t.Fatalf("create restore claim = %#v fresh=%v err=%v", created, fresh, err)
	}
	drift := claim
	drift.IdentityHash = "changed-identity"
	conflict, _, err := claimProfileRestore(t.Context(), fixture.database, drift)
	if !errors.Is(err, errProfileRestoreIdempotencyConflict) || conflict.ID != created.ID || conflict.Status != "running" {
		t.Fatalf("idempotency conflict identity = %#v err=%v", conflict, err)
	}
	parallel := claim
	parallel.IdempotencyKey = "second-key"
	parallel.IdentityHash = "second-identity"
	running, _, err := claimProfileRestore(t.Context(), fixture.database, parallel)
	if !errors.Is(err, errProfileRestoreInProgress) || running.ID != created.ID || running.Status != "running" {
		t.Fatalf("parallel conflict identity = %#v err=%v", running, err)
	}
}

func profileRestoreBody(t *testing.T, confirmation, filename, content string) (*bytes.Buffer, string) {
	return profileRestoreBodyWithKey(t, confirmation, filename, content, filename+"-attempt")
}

func profileRestoreBodyWithKey(t *testing.T, confirmation, filename, content, idempotencyKey string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("confirm_target", confirmation); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("idempotency_key", idempotencyKey); err != nil {
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

func TestProfileRestoreResultStatusFailsClosed(t *testing.T) {
	for _, test := range []struct {
		input connectors.ResultStatus
		want  string
		valid bool
	}{
		{connectors.ResultCompleted, "completed", true},
		{connectors.ResultOutcomeUnknown, "outcome_unknown", true},
		{connectors.ResultCanceled, "canceled", true},
		{connectors.ResultFailed, "failed", true},
		{"", "failed", false},
		{"invented", "failed", false},
	} {
		got, valid := profileRestoreResultStatus(test.input)
		if got != test.want || valid != test.valid {
			t.Fatalf("status %q = (%q, %t), want (%q, %t)", test.input, got, valid, test.want, test.valid)
		}
	}
}
