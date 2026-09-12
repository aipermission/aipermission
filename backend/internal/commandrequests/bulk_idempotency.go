package commandrequests

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const bulkIdempotencyTTL = 24 * time.Hour

var (
	ErrBulkIdempotencyNotFound = errors.New("bulk command idempotency record not found")
	ErrBulkIdempotencyConflict = errors.New("idempotency_key was already used for a different bulk command")
	ErrBulkIdempotencyExpired  = errors.New("the original bulk command result expired; retry with a new idempotency_key")
)

func BulkIdentityHash(targetIDs []int64, command, reason string) (string, error) {
	payload, err := json.Marshal(struct {
		TargetIDs []int64 `json:"target_ids"`
		Command   string  `json:"command"`
		Reason    string  `json:"reason"`
	}{TargetIDs: targetIDs, Command: command, Reason: reason})
	if err != nil {
		return "", fmt.Errorf("encode bulk command identity: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (r *Runtime) LookupBulk(ctx context.Context, key, identityHash string) (BulkHTTPResponse, error) {
	if err := r.validate(); err != nil {
		return BulkHTTPResponse{}, err
	}
	var storedHash, responseJSON, expiresAt string
	err := r.store.database.QueryRowContext(ctx, `
		SELECT identity_hash, response_json, expires_at
		FROM bulk_command_idempotency WHERE idempotency_key = ?`, key,
	).Scan(&storedHash, &responseJSON, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return BulkHTTPResponse{}, ErrBulkIdempotencyNotFound
	}
	if err != nil {
		return BulkHTTPResponse{}, err
	}
	if storedHash != identityHash {
		return BulkHTTPResponse{}, ErrBulkIdempotencyConflict
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || !expires.After(time.Now().UTC()) {
		return BulkHTTPResponse{}, ErrBulkIdempotencyExpired
	}
	var response BulkHTTPResponse
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		return BulkHTTPResponse{}, fmt.Errorf("decode bulk command replay: %w", err)
	}
	return response, nil
}

func (r *Runtime) InsertBulk(ctx context.Context, executor Executor, key, identityHash string, response BulkHTTPResponse) error {
	if err := r.validate(); err != nil {
		return err
	}
	if executor == nil {
		return ErrStoreUnavailable
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode bulk command replay: %w", err)
	}
	now := time.Now().UTC()
	if _, err := executor.ExecContext(ctx, `DELETE FROM bulk_command_idempotency WHERE expires_at <= ?`, now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("expire bulk command idempotency records: %w", err)
	}
	result, err := executor.ExecContext(ctx, `
		INSERT INTO bulk_command_idempotency (idempotency_key, identity_hash, response_json, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(idempotency_key) DO NOTHING`,
		key, identityHash, string(responseJSON), now.Format(time.RFC3339Nano), now.Add(bulkIdempotencyTTL).Format(time.RFC3339Nano),
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrBulkIdempotencyConflict
	}
	return nil
}
