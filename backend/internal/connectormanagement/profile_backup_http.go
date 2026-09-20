package connectormanagement

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const MaxProfileRestoreBodyBytes = 256 << 20
const maxProfileRestoreMultipartOverhead = 1 << 20
const profileRestoreAuditTimeout = 10 * time.Second

type ProfileBackupScope struct {
	Database         *sql.DB
	Registry         connectors.Catalog
	Runtime          CredentialRuntimePorts
	AcquireExclusive func(context.Context) (func(), error)
	Observe          func(context.Context, string, map[string]any)
	WithTransaction  func(context.Context, func(*sql.Tx, AuditAppender) error) error
}

type ProfileBackupScopeProvider func(http.ResponseWriter) (ProfileBackupScope, bool)

type ProfileBackupHTTPHandler struct{ scope ProfileBackupScopeProvider }

func NewProfileBackupHTTPHandler(scope ProfileBackupScopeProvider) *ProfileBackupHTTPHandler {
	return &ProfileBackupHTTPHandler{scope: scope}
}

func (h *ProfileBackupHTTPHandler) Download(w http.ResponseWriter, r *http.Request) {
	resolved, ok := h.resolveProfile(w, r)
	if !ok {
		return
	}
	backupRestorer, ok := resolved.connector.(connectors.BackupRestorer)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "connector does not support backup")
		return
	}
	artifact, err := backupRestorer.Backup(r.Context(), resolved.runtime, connectors.BackupRequest{Format: "sql"})
	if err != nil {
		writeProvisionError(w, err, resolved.boundary.Redact(resolved.scope.Runtime.RedactText(r.Context(), err.Error())))
		return
	}
	if len(artifact.Data) == 0 {
		httptransport.WriteError(w, http.StatusBadRequest, "connector returned an empty backup")
		return
	}
	filename := httpattachment.SafeFilename(artifact.Filename, "connector-backup.sql")
	contentType := strings.TrimSpace(artifact.ContentType)
	if contentType == "" {
		contentType = "application/sql"
	}
	httpattachment.SetHeaders(w, filename, contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Data)
	resolved.observe(r, "connector.profile.backup.downloaded", filename)
}

func (h *ProfileBackupHTTPHandler) Restore(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	release, ok := acquireLifecycleMutation(w, r, scope.AcquireExclusive, "connector profile restore was canceled")
	if !ok {
		return
	}
	defer release()
	resolved, ok := h.resolveProfileWithScope(w, r, scope)
	if !ok {
		return
	}
	backupRestorer, ok := resolved.connector.(connectors.BackupRestorer)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "connector does not support restore")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxProfileRestoreBodyBytes+maxProfileRestoreMultipartOverhead)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httptransport.WriteError(w, http.StatusRequestEntityTooLarge, "uploaded restore file is too large; maximum restore size is 256 MiB")
			return
		}
		httptransport.WriteError(w, http.StatusBadRequest, "invalid multipart restore upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if strings.TrimSpace(r.FormValue("confirm_target")) != resolved.target.Name {
		httptransport.WriteError(w, http.StatusBadRequest, "type the connector target name exactly to confirm restore")
		return
	}
	file, header, err := r.FormFile("dump")
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, "restore SQL file is required")
		return
	}
	defer file.Close()
	if status, message := profileRestoreSizeError(header.Size); status != 0 {
		httptransport.WriteError(w, status, message)
		return
	}
	artifactHash, err := profileRestoreArtifactHash(r.Context(), file, header.Size)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filename := httpattachment.SafeFilename(header.Filename, "restore.sql")
	claim := profileRestoreClaim{
		IdempotencyKey: r.FormValue("idempotency_key"), TargetID: resolved.target.ID,
		ProfileID: resolved.profile.ID, ConnectorKind: resolved.target.ConnectorKind,
		Filename: filename, ArtifactSHA256: artifactHash, SizeBytes: header.Size,
	}
	claim.IdentityHash = profileRestoreIdentity(claim.TargetID, claim.ProfileID, claim.ConnectorKind, claim.Filename, claim.ArtifactSHA256, claim.SizeBytes)
	operation, created, err := claimProfileRestore(r.Context(), resolved.scope.Database, claim)
	if err != nil {
		if errors.Is(err, errProfileRestoreInProgress) {
			writeRestoreError(w, http.StatusConflict, operation.ID, operation.Status, "restore_in_progress", err.Error())
			return
		}
		if errors.Is(err, errProfileRestoreIdempotencyConflict) {
			writeRestoreError(w, http.StatusConflict, operation.ID, operation.Status, "idempotency_conflict", err.Error())
			return
		}
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !created {
		resolved.writeRestoreReplay(w, operation)
		return
	}
	result, err := backupRestorer.Restore(r.Context(), resolved.runtime, connectors.RestoreRequest{
		Filename: header.Filename, Content: file, Size: header.Size,
	})
	if err != nil {
		status := restoreFailureStatus(err)
		errorCode := connectors.ErrorCode(err)
		if finishErr := resolved.finishRestore(r, operation.ID, filename, status, errorCode, ""); finishErr != nil {
			resolved.writeRestoreFinalizationFailure(w, r, operation.ID)
			return
		}
		writeRestoreExecutionError(w, operation.ID, status, errorCode, err, resolved.boundary.Redact(resolved.scope.Runtime.RedactText(r.Context(), err.Error())))
		return
	}
	status, valid := profileRestoreResultStatus(result.Status)
	if !valid || status != string(connectors.ResultCompleted) {
		errorCode := "invalid_connector_result"
		if valid {
			errorCode = string(result.Status)
		}
		if finishErr := resolved.finishRestore(r, operation.ID, filename, status, errorCode, ""); finishErr != nil {
			resolved.writeRestoreFinalizationFailure(w, r, operation.ID)
			return
		}
		writeRestoreResultError(w, operation.ID, status, errorCode)
		return
	}
	result, err = resolved.scope.Runtime.RedactResult(r.Context(), result, resolved.boundary)
	if err != nil {
		if finishErr := resolved.finishRestore(r, operation.ID, filename, status, "result_projection_failed", "result_redaction_failed"); finishErr != nil {
			resolved.writeRestoreFinalizationFailure(w, r, operation.ID)
			return
		}
		writeRestoreError(w, http.StatusConflict, operation.ID, status, "result_projection_failed",
			"restore completed but its response could not be safely projected; inspect the target before retrying")
		return
	}
	if err := resolved.finishRestore(r, operation.ID, filename, status, "", ""); err != nil {
		resolved.writeRestoreFinalizationFailure(w, r, operation.ID)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"operation_id": operation.ID, "status": status, "result": result})
}

func (resolved resolvedProfileBackup) writeRestoreFinalizationFailure(w http.ResponseWriter, r *http.Request, operationID int64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), profileRestoreAuditTimeout)
	defer cancel()
	operation, err := getProfileRestoreByID(ctx, resolved.scope.Database, operationID)
	if err == nil && operation.Status == "running" {
		if err = markProfileRestoreAuditPending(ctx, resolved.scope.Database, operationID); err == nil {
			operation, err = getProfileRestoreByID(ctx, resolved.scope.Database, operationID)
		}
	}
	if err != nil {
		writeRestoreAuditError(w, operationID)
		return
	}
	if operation.Status != "running" && !operation.AuditPending {
		resolved.writeRestoreReplay(w, operation)
		return
	}
	writeRestoreError(w, http.StatusConflict, operation.ID, operation.Status, operation.ErrorCode,
		"restore finished but its required audit record could not be confirmed; inspect the target before retrying")
}

func profileRestoreSizeError(size int64) (int, string) {
	if size <= 0 {
		return http.StatusBadRequest, "restore SQL file is empty"
	}
	if size > MaxProfileRestoreBodyBytes {
		return http.StatusRequestEntityTooLarge, "uploaded restore file is too large; maximum restore size is 256 MiB"
	}
	return 0, ""
}

func profileRestoreArtifactHash(ctx context.Context, file io.ReadSeeker, declaredSize int64) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("restore SQL file is not seekable")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(connectors.ReaderWithContext(ctx, file), MaxProfileRestoreBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("read restore SQL file: %w", err)
	}
	if written != declaredSize {
		return "", fmt.Errorf("restore SQL file size does not match the uploaded artifact")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind restore SQL file: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type resolvedProfileBackup struct {
	scope     ProfileBackupScope
	target    connectortargets.Target
	profile   connectortargets.CredentialProfile
	connector connectors.Connector
	runtime   connectors.RuntimeContext
	boundary  actionresult.CredentialBoundary
}

func (h *ProfileBackupHTTPHandler) resolveProfile(w http.ResponseWriter, r *http.Request) (resolvedProfileBackup, bool) {
	scope, ok := h.resolve(w)
	if !ok {
		return resolvedProfileBackup{}, false
	}
	return h.resolveProfileWithScope(w, r, scope)
}

func (h *ProfileBackupHTTPHandler) resolveProfileWithScope(w http.ResponseWriter, r *http.Request, scope ProfileBackupScope) (resolvedProfileBackup, bool) {
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return resolvedProfileBackup{}, false
	}
	profileID, ok := httptransport.ParsePathInt64(w, r, "profile_id", "profile_id is required")
	if !ok {
		return resolvedProfileBackup{}, false
	}
	store := connectortargets.NewStore(scope.Database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return resolvedProfileBackup{}, false
	}
	profile, err := store.GetCredentialProfile(r.Context(), targetID, profileID)
	if err != nil {
		writeTargetError(w, err)
		return resolvedProfileBackup{}, false
	}
	connector, ok := scope.Registry.Get(target.ConnectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return resolvedProfileBackup{}, false
	}
	secrets, err := decryptCredentialSecrets(r.Context(), profile, scope.Runtime.DecryptSecret)
	if err != nil {
		httptransport.WriteInternalError(w)
		return resolvedProfileBackup{}, false
	}
	boundary := actionresult.NewCredentialBoundary(secrets)
	return resolvedProfileBackup{
		scope: scope, target: target, profile: profile, connector: connector,
		runtime: scope.Runtime.RuntimeContext(target, profile, secrets, boundary), boundary: boundary,
	}, true
}

func (h *ProfileBackupHTTPHandler) resolve(w http.ResponseWriter) (ProfileBackupScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return ProfileBackupScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return ProfileBackupScope{}, false
	}
	if scope.Database == nil || scope.Registry == nil || !scope.Runtime.valid() || scope.AcquireExclusive == nil || scope.Observe == nil || scope.WithTransaction == nil {
		httptransport.WriteInternalError(w)
		return ProfileBackupScope{}, false
	}
	return scope, true
}

func (resolved resolvedProfileBackup) observe(r *http.Request, action, filename string) {
	resolved.scope.Observe(r.Context(), action, map[string]any{
		"target_id": resolved.target.ID, "profile_id": resolved.profile.ID,
		"connector_kind": resolved.target.ConnectorKind, "filename": filename,
	})
}

func (resolved resolvedProfileBackup) finishRestore(r *http.Request, operationID int64, filename, status, errorCode, projectionErrorCode string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), profileRestoreAuditTimeout)
	defer cancel()
	return resolved.scope.WithTransaction(ctx, func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errors.New("profile restore transaction runtime is unavailable")
		}
		if err := finishProfileRestore(ctx, tx, operationID, status, errorCode); err != nil {
			return err
		}
		payload := map[string]any{
			"target_id": resolved.target.ID, "profile_id": resolved.profile.ID,
			"connector_kind": resolved.target.ConnectorKind,
			"operation_id":   operationID,
			"filename":       httpattachment.SafeFilename(filename, "restore.sql"),
			"status":         status,
		}
		if errorCode != "" {
			payload["error_code"] = errorCode
		}
		if projectionErrorCode != "" {
			payload["projection_error_code"] = projectionErrorCode
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.backup.restore_finished", payload)
	})
}

func (resolved resolvedProfileBackup) writeRestoreReplay(w http.ResponseWriter, operation profileRestoreOperation) {
	switch operation.Status {
	case "completed":
		if operation.ErrorCode != "" {
			writeRestoreError(w, http.StatusConflict, operation.ID, operation.Status, operation.ErrorCode,
				"the restore completed but its reporting boundary failed; inspect the target before starting another attempt")
			return
		}
		httptransport.WriteJSON(w, http.StatusOK, map[string]any{
			"operation_id": operation.ID, "status": operation.Status, "replayed": true,
		})
	case "pending", "running":
		writeRestoreError(w, http.StatusConflict, operation.ID, operation.Status, "restore_in_progress",
			"this restore operation is already in progress; inspect its audit record before retrying")
	default:
		writeRestoreError(w, http.StatusConflict, operation.ID, operation.Status, operation.ErrorCode,
			"this restore attempt is already terminal; inspect the target and use a new attempt only after reconciliation")
	}
}

func profileRestoreResultStatus(status connectors.ResultStatus) (string, bool) {
	switch status {
	case connectors.ResultCompleted:
		return string(status), true
	case connectors.ResultOutcomeUnknown:
		return string(status), true
	case connectors.ResultFailed, connectors.ResultError, connectors.ResultBlocked,
		connectors.ResultCanceled, connectors.ResultStale, connectors.ResultDeclined:
		if status == connectors.ResultCanceled {
			return "canceled", true
		}
		return "failed", true
	default:
		return "failed", false
	}
}

func writeRestoreResultError(w http.ResponseWriter, operationID int64, status, code string) {
	httpStatus := http.StatusBadGateway
	message := "connector did not complete the restore"
	if status == string(connectors.ResultOutcomeUnknown) {
		httpStatus = http.StatusConflict
		message = "restore outcome could not be confirmed; inspect the target before retrying"
	}
	writeRestoreError(w, httpStatus, operationID, status, code, message)
}

func writeRestoreExecutionError(w http.ResponseWriter, operationID int64, status, code string, err error, message string) {
	httpStatus := http.StatusBadRequest
	if status == string(connectors.ResultOutcomeUnknown) {
		httpStatus = http.StatusConflict
	} else if connectors.ErrorStatus(err) == connectors.ResultBlocked {
		httpStatus = http.StatusForbidden
	}
	writeRestoreError(w, httpStatus, operationID, status, code, message)
}

func writeRestoreError(w http.ResponseWriter, httpStatus int, operationID int64, status, code, message string) {
	httptransport.WriteJSON(w, httpStatus, map[string]any{
		"error": message, "code": strings.TrimSpace(code),
		"operation_id": operationID, "status": strings.TrimSpace(status),
	})
}

func restoreFailureStatus(err error) string {
	if status := connectors.ErrorStatus(err); status != "" {
		if normalized, valid := profileRestoreResultStatus(status); valid {
			return normalized
		}
		return "failed"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return string(connectors.ResultOutcomeUnknown)
	}
	return string(connectors.ResultFailed)
}

func writeRestoreAuditError(w http.ResponseWriter, operationID int64) {
	writeRestoreError(w, http.StatusConflict, operationID, string(connectors.ResultOutcomeUnknown), "audit_persistence_failed",
		"restore finished but its required audit record could not be persisted; inspect the target before retrying")
}
