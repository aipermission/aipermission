package backups

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups/uploadoperation"
)

var ErrUploadIdempotencyConflict = errors.New("backup upload idempotency key was already used for a different request")
var ErrUploadResultExpired = errors.New("the original backup upload record is no longer available")

type UploadOperation = uploadoperation.Operation
type ClaimUploadOperationRequest = uploadoperation.ClaimRequest

func (s *Store) ClaimUploadOperation(ctx context.Context, request ClaimUploadOperationRequest) (UploadOperation, bool, error) {
	request, err := uploadoperation.NormalizeClaim(request)
	if err != nil {
		return UploadOperation{}, false, ValidationError(err.Error())
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO backup_upload_operations (
			idempotency_key, provider_id, database_id, workspace_instance_id, stream_id, source_installation_id,
			status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING`,
		request.IdempotencyKey, request.ProviderID, request.DatabaseID, request.WorkspaceInstanceID, request.StreamID,
		request.SourceInstallationID, now, now,
	)
	if err != nil {
		return UploadOperation{}, false, fmt.Errorf("claim backup upload operation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return UploadOperation{}, false, fmt.Errorf("read backup upload claim result: %w", err)
	}
	operation, err := s.GetUploadOperation(ctx, request.IdempotencyKey)
	if err != nil {
		return UploadOperation{}, false, err
	}
	if operation.ProviderID != request.ProviderID || operation.WorkspaceInstanceID != request.WorkspaceInstanceID ||
		operation.StreamID != request.StreamID || operation.SourceInstallationID != request.SourceInstallationID {
		return UploadOperation{}, false, ErrUploadIdempotencyConflict
	}
	return operation, rows == 1, nil
}

func (s *Store) GetUploadOperation(ctx context.Context, key string) (UploadOperation, error) {
	var operation UploadOperation
	err := s.db.QueryRowContext(ctx, `
		SELECT idempotency_key, provider_id, database_id, workspace_instance_id, stream_id, source_installation_id,
		       status, provider_file_id, last_error, created_at, updated_at, completed_at
		FROM backup_upload_operations WHERE idempotency_key = ?`, strings.TrimSpace(key)).Scan(
		&operation.IdempotencyKey, &operation.ProviderID, &operation.DatabaseID, &operation.WorkspaceInstanceID,
		&operation.StreamID, &operation.SourceInstallationID, &operation.Status, &operation.ProviderFileID,
		&operation.LastError, &operation.CreatedAt, &operation.UpdatedAt, &operation.CompletedAt,
	)
	if err != nil {
		return UploadOperation{}, fmt.Errorf("read backup upload operation: %w", err)
	}
	return operation, nil
}

func (s *Store) MarkUploadDispatched(ctx context.Context, key string) error {
	return s.updateUploadOperation(ctx, key, "dispatched", "", false)
}

func (s *Store) MarkUploadOutcomeUnknown(ctx context.Context, key string, operationErr error) error {
	message := "backup upload outcome could not be confirmed"
	if operationErr != nil {
		message = operationErr.Error()
	}
	return s.updateUploadOperation(ctx, key, "outcome_unknown", message, false)
}

func (s *Store) MarkUploadExpired(ctx context.Context, key string) error {
	return s.updateUploadOperation(ctx, key, "expired", "remote upload result expired", true)
}

func (s *Store) ExpireUnresolvedUploadOperations(ctx context.Context, providerID int64, message string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		UPDATE backup_upload_operations
		SET status = 'expired', last_error = ?, updated_at = ?, completed_at = COALESCE(completed_at, ?)
		WHERE provider_id = ? AND status IN ('pending', 'dispatched', 'outcome_unknown')`,
		message, now, now, providerID,
	)
	if err != nil {
		return fmt.Errorf("expire unresolved backup upload operations: %w", err)
	}
	return nil
}

func (s *Store) updateUploadOperation(ctx context.Context, key, status, message string, terminal bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var completedAt any
	if terminal {
		completedAt = now
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE backup_upload_operations
		SET status = ?, last_error = ?, updated_at = ?, completed_at = COALESCE(?, completed_at)
		WHERE idempotency_key = ? AND status != 'completed' AND (status != 'expired' OR ? = 'expired')`,
		status, message, now, completedAt, strings.TrimSpace(key), status)
	if err != nil {
		return fmt.Errorf("update backup upload operation: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("read backup upload update result: %w", rowsErr)
	} else if rows != 1 {
		operation, getErr := s.GetUploadOperation(ctx, key)
		if getErr != nil || (operation.Status != "completed" && operation.Status != status) {
			return fmt.Errorf("backup upload operation is not active")
		}
	}
	return nil
}

func (s *Store) CompleteUploadOperation(ctx context.Context, key, providerFileID string) error {
	key = strings.TrimSpace(key)
	providerFileID = strings.TrimSpace(providerFileID)
	if key == "" || providerFileID == "" {
		return ValidationError("backup upload completion identity is incomplete")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		UPDATE backup_upload_operations
		SET status = 'completed', provider_file_id = ?, last_error = '', updated_at = ?, completed_at = ?
		WHERE idempotency_key = ? AND status IN ('pending', 'dispatched', 'outcome_unknown')`,
		providerFileID, now, now, key,
	)
	if err != nil {
		return fmt.Errorf("complete backup upload operation: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("read backup upload completion result: %w", rowsErr)
	} else if rows != 1 {
		operation, getErr := s.GetUploadOperation(ctx, key)
		if getErr != nil {
			return getErr
		}
		if operation.Status != "completed" || operation.ProviderFileID != providerFileID {
			return fmt.Errorf("backup upload operation is not completable")
		}
	}
	return nil
}

func (s *Store) GetRecordByProviderFileID(ctx context.Context, providerID int64, providerFileID string) (Record, error) {
	record, err := s.getRecordByProviderFileID(ctx, providerID, strings.TrimSpace(providerFileID))
	if errors.Is(err, ErrRecordNotFound) {
		return Record{}, ErrUploadResultExpired
	}
	return record, err
}
