package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	IdempotencyResourceTransfer = "transfer"
	IdempotencyResourceBatch    = "batch"
	MaxIdempotencyKeyBytes      = 128
	idempotencyRetention        = 30 * 24 * time.Hour
)

var (
	ErrIdempotencyConflict      = errors.New("file transfer idempotency key was already used for a different request")
	ErrIdempotencyNotFound      = errors.New("file transfer idempotency key not found")
	ErrIdempotencyResultExpired = errors.New("file transfer idempotency result expired")
)

type IdempotencyClaim struct {
	Scope        string
	Key          string
	IdentityHash string
	ResourceKind string
}

func (s *Store) GetIdempotentTransfer(ctx context.Context, claim IdempotencyClaim) (Record, error) {
	claim, err := normalizeIdempotencyClaim(claim, IdempotencyResourceTransfer)
	if err != nil {
		return Record{}, err
	}
	resourceID, err := s.getIdempotencyResourceID(ctx, claim)
	if err != nil {
		return Record{}, err
	}
	item, err := s.createdTransfer(ctx, resourceID)
	if errors.Is(err, ErrNotFound) {
		return Record{}, ErrIdempotencyResultExpired
	}
	return item, err
}

func (s *Store) GetIdempotentBatch(ctx context.Context, claim IdempotencyClaim) (BatchRecord, error) {
	claim, err := normalizeIdempotencyClaim(claim, IdempotencyResourceBatch)
	if err != nil {
		return BatchRecord{}, err
	}
	resourceID, err := s.getIdempotencyResourceID(ctx, claim)
	if err != nil {
		return BatchRecord{}, err
	}
	batch, err := s.createdBatch(ctx, resourceID)
	if errors.Is(err, ErrNotFound) {
		return BatchRecord{}, ErrIdempotencyResultExpired
	}
	return batch, err
}

func (s *Store) getIdempotencyResourceID(ctx context.Context, claim IdempotencyClaim) (int64, error) {
	var record idempotencyRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT identity_hash, resource_kind, resource_id
		FROM file_transfer_start_idempotency
		WHERE scope = ? AND idempotency_key = ?`, claim.Scope, claim.Key).Scan(
		&record.IdentityHash, &record.ResourceKind, &record.ResourceID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrIdempotencyNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read file transfer idempotency result: %w", err)
	}
	if record.IdentityHash != claim.IdentityHash || record.ResourceKind != claim.ResourceKind {
		return 0, ErrIdempotencyConflict
	}
	if record.ResourceID < 1 {
		return 0, ErrIdempotencyResultExpired
	}
	return record.ResourceID, nil
}

type idempotencyRecord struct {
	IdentityHash string
	ResourceKind string
	ResourceID   int64
}

func normalizeIdempotencyClaim(claim IdempotencyClaim, expectedKind string) (IdempotencyClaim, error) {
	claim.Scope = strings.TrimSpace(claim.Scope)
	claim.Key = strings.TrimSpace(claim.Key)
	claim.IdentityHash = strings.TrimSpace(claim.IdentityHash)
	claim.ResourceKind = strings.TrimSpace(claim.ResourceKind)
	if claim.Scope == "" || claim.Key == "" || claim.IdentityHash == "" {
		return claim, fmt.Errorf("idempotency scope, key, and identity hash are required")
	}
	if len(claim.Key) > MaxIdempotencyKeyBytes {
		return claim, fmt.Errorf("idempotency key is too long")
	}
	if claim.ResourceKind != expectedKind {
		return claim, fmt.Errorf("idempotency resource kind must be %s", expectedKind)
	}
	return claim, nil
}

func findIdempotencyRecord(ctx context.Context, tx *sql.Tx, claim IdempotencyClaim) (idempotencyRecord, error) {
	var record idempotencyRecord
	err := tx.QueryRowContext(ctx, `
		SELECT identity_hash, resource_kind, resource_id
		FROM file_transfer_start_idempotency
		WHERE scope = ? AND idempotency_key = ?`, claim.Scope, claim.Key).Scan(
		&record.IdentityHash, &record.ResourceKind, &record.ResourceID,
	)
	return record, err
}

func claimIdempotencyKey(ctx context.Context, tx *sql.Tx, claim IdempotencyClaim) (idempotencyRecord, bool, error) {
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO file_transfer_start_idempotency (
			scope, idempotency_key, identity_hash, resource_kind, resource_id, created_at, expires_at
		) VALUES (?, ?, ?, ?, 0, ?, ?)`,
		claim.Scope, claim.Key, claim.IdentityHash, claim.ResourceKind,
		now.Format(time.RFC3339Nano), now.Add(idempotencyRetention).Format(time.RFC3339Nano),
	)
	if err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("claim file transfer idempotency key: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("read file transfer idempotency claim rows: %w", err)
	}
	if rows == 1 {
		return idempotencyRecord{}, true, nil
	}
	record, err := findIdempotencyRecord(ctx, tx, claim)
	if err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("read existing file transfer idempotency claim: %w", err)
	}
	if record.IdentityHash != claim.IdentityHash || record.ResourceKind != claim.ResourceKind {
		return idempotencyRecord{}, false, ErrIdempotencyConflict
	}
	if record.ResourceID < 1 {
		return idempotencyRecord{}, false, ErrIdempotencyResultExpired
	}
	return record, false, nil
}

func completeIdempotencyClaim(ctx context.Context, tx *sql.Tx, claim IdempotencyClaim, resourceID int64) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE file_transfer_start_idempotency
		SET resource_id = ?
		WHERE scope = ? AND idempotency_key = ? AND identity_hash = ? AND resource_kind = ? AND resource_id = 0`,
		resourceID, claim.Scope, claim.Key, claim.IdentityHash, claim.ResourceKind,
	)
	if err != nil {
		return fmt.Errorf("complete file transfer idempotency claim: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read file transfer idempotency claim rows: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("complete file transfer idempotency claim: unexpected row count %d", rows)
	}
	return nil
}
