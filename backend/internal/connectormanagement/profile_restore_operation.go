package connectormanagement

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
)

var (
	errProfileRestoreIdempotencyConflict = errors.New("restore idempotency key was already used for a different artifact")
	errProfileRestoreInProgress          = errors.New("another restore is already running for this target profile")
)

type profileRestoreOperation struct {
	ID             int64
	IdempotencyKey string
	IdentityHash   string
	Status         string
	ErrorCode      string
	AuditPending   bool
}

type profileRestoreClaim struct {
	IdempotencyKey string
	IdentityHash   string
	TargetID       int64
	ProfileID      int64
	ConnectorKind  string
	Filename       string
	ArtifactSHA256 string
	SizeBytes      int64
}

type profileRestoreRecovery struct {
	ID            int64
	TargetID      int64
	ProfileID     int64
	ConnectorKind string
	Filename      string
	Status        string
	ErrorCode     string
	AuditPending  bool
}

type profileRestoreExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func profileRestoreIdentity(targetID, profileID int64, connectorKind, filename, artifactSHA256 string, size int64) string {
	value := strings.Join([]string{
		strconv.FormatInt(targetID, 10), strconv.FormatInt(profileID, 10),
		strings.TrimSpace(connectorKind), strings.TrimSpace(filename),
		strings.TrimSpace(artifactSHA256), strconv.FormatInt(size, 10),
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func claimProfileRestore(ctx context.Context, database *sql.DB, claim profileRestoreClaim) (profileRestoreOperation, bool, error) {
	claim.IdempotencyKey = strings.TrimSpace(claim.IdempotencyKey)
	if claim.IdempotencyKey == "" || len(claim.IdempotencyKey) > 128 {
		return profileRestoreOperation{}, false, fmt.Errorf("idempotency_key must contain 1 to 128 characters")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := database.ExecContext(ctx, `
		INSERT INTO profile_restore_operations (
			idempotency_key, identity_hash, target_id, profile_id, connector_kind,
			filename, artifact_sha256, size_bytes, status, created_at, updated_at
		)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, 'running', ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM profile_restore_operations
			WHERE target_id = ? AND profile_id = ? AND status = 'running'
		)
		AND NOT EXISTS (
			SELECT 1 FROM profile_restore_idempotency_tombstones
			WHERE idempotency_key = ? AND julianday(expires_at) > julianday('now')
		)
		ON CONFLICT(idempotency_key) DO NOTHING`,
		claim.IdempotencyKey, claim.IdentityHash, claim.TargetID, claim.ProfileID,
		claim.ConnectorKind, claim.Filename, claim.ArtifactSHA256, claim.SizeBytes, now, now,
		claim.TargetID, claim.ProfileID, claim.IdempotencyKey,
	)
	if err != nil {
		return profileRestoreOperation{}, false, fmt.Errorf("claim profile restore: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return profileRestoreOperation{}, false, fmt.Errorf("read profile restore claim: %w", err)
	}
	operation, err := getProfileRestore(ctx, database, claim.IdempotencyKey)
	if err != nil {
		if rows == 0 && errors.Is(err, sql.ErrNoRows) {
			var runningID int64
			if runningErr := database.QueryRowContext(ctx, `
				SELECT id FROM profile_restore_operations
				WHERE target_id = ? AND profile_id = ? AND status = 'running'
				ORDER BY id LIMIT 1`, claim.TargetID, claim.ProfileID).Scan(&runningID); runningErr == nil {
				running, loadErr := getProfileRestoreByID(ctx, database, runningID)
				if loadErr != nil {
					return profileRestoreOperation{}, false, loadErr
				}
				return running, false, errProfileRestoreInProgress
			}
		}
		return profileRestoreOperation{}, false, err
	}
	if operation.IdentityHash != claim.IdentityHash {
		return operation, false, errProfileRestoreIdempotencyConflict
	}
	return operation, rows == 1, nil
}

func getProfileRestore(ctx context.Context, database *sql.DB, key string) (profileRestoreOperation, error) {
	operation, err := scanProfileRestore(database.QueryRowContext(ctx, `
		SELECT id, idempotency_key, identity_hash, status, error_code, audit_pending
		FROM profile_restore_operations WHERE idempotency_key = ?`, strings.TrimSpace(key)))
	if !errors.Is(err, sql.ErrNoRows) {
		return operation, err
	}
	return scanProfileRestore(database.QueryRowContext(ctx, `
		SELECT operation_id, idempotency_key, identity_hash, status, error_code, 0
		FROM profile_restore_idempotency_tombstones
		WHERE idempotency_key = ? AND julianday(expires_at) > julianday('now')`, strings.TrimSpace(key)))
}

func getProfileRestoreByID(ctx context.Context, database *sql.DB, id int64) (profileRestoreOperation, error) {
	return scanProfileRestore(database.QueryRowContext(ctx, `
		SELECT id, idempotency_key, identity_hash, status, error_code, audit_pending
		FROM profile_restore_operations WHERE id = ?`, id))
}

type profileRestoreScanner interface{ Scan(...any) error }

func scanProfileRestore(row profileRestoreScanner) (profileRestoreOperation, error) {
	var operation profileRestoreOperation
	err := row.Scan(
		&operation.ID, &operation.IdempotencyKey, &operation.IdentityHash,
		&operation.Status, &operation.ErrorCode, &operation.AuditPending,
	)
	if err != nil {
		return profileRestoreOperation{}, fmt.Errorf("read profile restore: %w", err)
	}
	return operation, nil
}

func markProfileRestoreAuditPending(ctx context.Context, database *sql.DB, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := database.ExecContext(ctx, `
		UPDATE profile_restore_operations
		SET status = 'outcome_unknown', error_code = 'audit_persistence_failed',
			audit_pending = 1, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status = 'running'`, now, now, id)
	if err != nil {
		return fmt.Errorf("persist profile restore audit recovery: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read profile restore audit recovery update: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("profile restore operation is not running")
	}
	return nil
}

func finishProfileRestore(ctx context.Context, executor profileRestoreExecutor, id int64, status, errorCode string) error {
	if status != "completed" && status != "failed" && status != "canceled" && status != "outcome_unknown" {
		return fmt.Errorf("invalid profile restore terminal status %q", status)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := executor.ExecContext(ctx, `
		UPDATE profile_restore_operations
		SET status = ?, error_code = ?, audit_pending = 0, completed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'running'`, status, strings.TrimSpace(errorCode), now, now, id)
	if err != nil {
		return fmt.Errorf("finish profile restore: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read profile restore completion: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("profile restore operation is not running")
	}
	return nil
}

func RecoverProfileRestores(ctx context.Context, withTransaction func(context.Context, func(*sql.Tx, AuditAppender) error) error) error {
	if withTransaction == nil {
		return fmt.Errorf("profile restore recovery transaction is unavailable")
	}
	return withTransaction(ctx, func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return fmt.Errorf("profile restore recovery runtime is unavailable")
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT id, target_id, profile_id, connector_kind, filename, status, error_code, audit_pending
			FROM profile_restore_operations WHERE status = 'running' OR audit_pending = 1 ORDER BY id`)
		if err != nil {
			return fmt.Errorf("list running profile restores: %w", err)
		}
		recoveries := []profileRestoreRecovery{}
		for rows.Next() {
			var recovery profileRestoreRecovery
			if err := rows.Scan(
				&recovery.ID, &recovery.TargetID, &recovery.ProfileID, &recovery.ConnectorKind,
				&recovery.Filename, &recovery.Status, &recovery.ErrorCode, &recovery.AuditPending,
			); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan running profile restore: %w", err)
			}
			recoveries = append(recoveries, recovery)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close running profile restores: %w", err)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate running profile restores: %w", err)
		}
		for _, recovery := range recoveries {
			if recovery.Status == "running" {
				recovery.Status = string(connectors.ResultOutcomeUnknown)
				recovery.ErrorCode = "gateway_restarted"
				if err := finishProfileRestore(ctx, tx, recovery.ID, recovery.Status, recovery.ErrorCode); err != nil {
					return err
				}
			}
			if err := appendAudit(tx, "gateway", nil, 0, "connector.profile.backup.restore_finished", map[string]any{
				"target_id": recovery.TargetID, "profile_id": recovery.ProfileID,
				"connector_kind": recovery.ConnectorKind, "operation_id": recovery.ID,
				"filename": httpattachment.SafeFilename(recovery.Filename, "restore.sql"),
				"status":   recovery.Status, "error_code": recovery.ErrorCode,
			}); err != nil {
				return err
			}
			if recovery.AuditPending {
				if _, err := tx.ExecContext(ctx, `UPDATE profile_restore_operations SET audit_pending = 0 WHERE id = ?`, recovery.ID); err != nil {
					return fmt.Errorf("complete profile restore audit recovery: %w", err)
				}
			}
		}
		return nil
	})
}
