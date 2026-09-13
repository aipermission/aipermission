package backups

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrUploadIdempotencyConflict = errors.New("backup upload idempotency key was already used for a different request")
var ErrUploadResultExpired = errors.New("the original backup upload record is no longer available")

const maxUploadIdempotencyKeyBytes = 128

type UploadOperation struct {
	IdempotencyKey       string
	ProviderID           int64
	DatabaseID           string
	StreamID             string
	SourceInstallationID string
	Status               string
	ProviderFileID       string
	LastError            string
	CreatedAt            string
	UpdatedAt            string
	CompletedAt          *string
}

type ClaimUploadOperationRequest struct {
	IdempotencyKey       string
	ProviderID           int64
	DatabaseID           string
	StreamID             string
	SourceInstallationID string
}

func (s *Store) ClaimUploadOperation(ctx context.Context, request ClaimUploadOperationRequest) (UploadOperation, bool, error) {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.DatabaseID = strings.TrimSpace(request.DatabaseID)
	request.StreamID = strings.TrimSpace(request.StreamID)
	request.SourceInstallationID = strings.TrimSpace(request.SourceInstallationID)
	if request.IdempotencyKey == "" || len(request.IdempotencyKey) > maxUploadIdempotencyKeyBytes {
		return UploadOperation{}, false, ValidationError("idempotency_key must contain 1 to 128 characters")
	}
	if request.ProviderID < 1 || request.DatabaseID == "" || request.StreamID == "" || request.SourceInstallationID == "" {
		return UploadOperation{}, false, ValidationError("backup upload identity is incomplete")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO backup_upload_operations (
			idempotency_key, provider_id, database_id, stream_id, source_installation_id,
			status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING`,
		request.IdempotencyKey, request.ProviderID, request.DatabaseID, request.StreamID,
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
	if operation.ProviderID != request.ProviderID || operation.DatabaseID != request.DatabaseID || operation.StreamID != request.StreamID || operation.SourceInstallationID != request.SourceInstallationID {
		return UploadOperation{}, false, ErrUploadIdempotencyConflict
	}
	return operation, rows == 1, nil
}

func (s *Store) GetUploadOperation(ctx context.Context, key string) (UploadOperation, error) {
	var operation UploadOperation
	err := s.db.QueryRowContext(ctx, `
		SELECT idempotency_key, provider_id, database_id, stream_id, source_installation_id,
		       status, provider_file_id, last_error, created_at, updated_at, completed_at
		FROM backup_upload_operations WHERE idempotency_key = ?`, strings.TrimSpace(key)).Scan(
		&operation.IdempotencyKey, &operation.ProviderID, &operation.DatabaseID, &operation.StreamID,
		&operation.SourceInstallationID, &operation.Status, &operation.ProviderFileID,
		&operation.LastError, &operation.CreatedAt, &operation.UpdatedAt, &operation.CompletedAt,
	)
	if err != nil {
		return UploadOperation{}, fmt.Errorf("read backup upload operation: %w", err)
	}
	return operation, nil
}

func (s *Store) MarkUploadDispatched(ctx context.Context, key string) error {
	return s.updateUploadOperation(ctx, key, "dispatched", "")
}

func (s *Store) MarkUploadOutcomeUnknown(ctx context.Context, key string, operationErr error) error {
	message := "backup upload outcome could not be confirmed"
	if operationErr != nil {
		message = operationErr.Error()
	}
	return s.updateUploadOperation(ctx, key, "outcome_unknown", message)
}

func (s *Store) updateUploadOperation(ctx context.Context, key, status, message string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		UPDATE backup_upload_operations SET status = ?, last_error = ?, updated_at = ?
		WHERE idempotency_key = ? AND status != 'completed'`, status, message, now, strings.TrimSpace(key))
	if err != nil {
		return fmt.Errorf("update backup upload operation: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("read backup upload update result: %w", rowsErr)
	} else if rows != 1 {
		operation, getErr := s.GetUploadOperation(ctx, key)
		if getErr != nil || operation.Status != "completed" {
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
