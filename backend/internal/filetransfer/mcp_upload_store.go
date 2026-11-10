package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// CreateMCPUpload binds a private staged file to the authenticated token's
// idempotency scope. Quota, expiry, record and history commit together.
func (s *Store) CreateMCPUpload(ctx context.Context, stage MCPUploadStage, now time.Time) (Record, bool, error) {
	claim, err := MCPUploadClaim(stage.TokenID, stage.IdempotencyKey, stage.Identity)
	if err != nil {
		return Record{}, false, err
	}
	if !filepath.IsAbs(stage.TempPath) || filepath.Clean(stage.TempPath) != stage.TempPath {
		return Record{}, false, errors.New("MCP upload staging path must be canonical and absolute")
	}
	manifest := stage.Identity.Manifest
	request, err := normalizeCreateRequest(CreateRequest{
		RuntimeID: stage.RuntimeID, Source: SourceMCP, Direction: DirectionUpload,
		LocalPath: manifest.FileName, FileName: manifest.FileName, RemotePath: manifest.RemotePath,
		SizeBytes: manifest.SizeBytes, ChecksumSHA256: manifest.ChecksumSHA256, TempPath: stage.TempPath,
	})
	if err != nil {
		return Record{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, false, err
	}
	defer tx.Rollback()
	existing, created, err := claimIdempotencyKey(ctx, tx, claim)
	if err != nil {
		return Record{}, false, err
	}
	if err := validateMCPUploadRuntime(ctx, tx, stage.RuntimeID, stage.Identity.TargetRef); err != nil {
		return Record{}, false, err
	}
	if !created {
		_ = tx.Rollback()
		item, err := s.createdTransfer(ctx, existing.ResourceID)
		if errors.Is(err, ErrNotFound) {
			return Record{}, false, ErrIdempotencyResultExpired
		}
		return item, false, err
	}
	var count int
	var pathOwned bool
	if err := tx.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM file_transfers WHERE temp_path = ?) OR
		EXISTS (SELECT 1 FROM file_transfer_batches WHERE archive_path = ?)`, request.TempPath, request.TempPath).Scan(&pathOwned); err != nil {
		return Record{}, false, err
	}
	if pathOwned {
		return Record{}, false, fmt.Errorf("%w: upload staging file already has an owner", ErrInvalidArgument)
	}
	var size int64
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size_bytes), 0)
		FROM file_transfers WHERE source = ? AND direction = ? AND temp_path != ''`, SourceMCP, DirectionUpload).Scan(&count, &size)
	if err != nil {
		return Record{}, false, err
	}
	if count >= MaxMCPUploadStagedFiles || size > MaxMCPUploadStagedBytes-request.SizeBytes {
		return Record{}, false, ErrMCPUploadCapacity
	}
	id, err := insertTransfer(ctx, tx, request, StatusPending, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return Record{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file_transfers SET temp_expires_at = ? WHERE id = ?`, now.UTC().Add(MCPUploadTTL).Format(mcpUploadTimeLayout), id); err != nil {
		return Record{}, false, err
	}
	if err := syncTransferHistoryWithExecutor(ctx, tx, id); err != nil {
		return Record{}, false, err
	}
	if err := completeIdempotencyClaim(ctx, tx, claim, id); err != nil {
		return Record{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Record{}, false, err
	}
	item, err := s.createdTransfer(ctx, id)
	return item, true, err
}

func (s *Store) GetMCPUpload(ctx context.Context, tokenID int64, key string, identity UploadIdentity) (Record, error) {
	claim, err := MCPUploadClaim(tokenID, key, identity)
	if err != nil {
		return Record{}, ErrNotFound
	}
	item, err := s.GetIdempotentTransfer(ctx, claim)
	if errors.Is(err, ErrIdempotencyNotFound) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	if item.Source != SourceMCP || item.Direction != DirectionUpload || item.BatchID != 0 {
		return Record{}, ErrNotFound
	}
	if err := validateMCPUploadRuntime(ctx, s.db, item.RuntimeID, identity.TargetRef); err != nil {
		return Record{}, err
	}
	manifest := identity.Manifest
	if item.FileName != manifest.FileName || item.RemotePath != manifest.RemotePath || item.SizeBytes != manifest.SizeBytes || item.ChecksumSHA256 != manifest.ChecksumSHA256 {
		return Record{}, ErrIdempotencyConflict
	}
	return item, nil
}

func (s *Store) MCPUploadStatus(ctx context.Context, tokenID int64, key, targetRef, actionName string) (Record, int64, error) {
	if tokenID < 1 || key == "" || len(key) > MaxIdempotencyKeyBytes {
		return Record{}, 0, ErrNotFound
	}
	kind, targetID, profileID, valid := connectors.ParseTargetRef(targetRef)
	if !valid || !connectors.ValidIdentifier(actionName) {
		return Record{}, 0, ErrNotFound
	}
	var id, requestID int64
	err := s.db.QueryRowContext(ctx, `SELECT i.resource_id, COALESCE(a.id, 0)
		FROM file_transfer_start_idempotency i
		LEFT JOIN connector_action_requests a ON a.token_id = ? AND a.source = 'mcp'
			AND a.idempotency_key = i.idempotency_key AND a.connector_kind = ? AND a.target_id = ?
			AND a.profile_id = ? AND a.action_name = ?
		WHERE i.scope = ? AND i.idempotency_key = ? AND i.resource_kind = ?`,
		tokenID, kind, targetID, profileID, actionName, mcpUploadScope(tokenID), key, IdempotencyResourceTransfer).Scan(&id, &requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, 0, ErrNotFound
	}
	if err != nil {
		return Record{}, 0, err
	}
	item, err := s.createdTransfer(ctx, id)
	if err != nil {
		return Record{}, 0, err
	}
	if item.Source != SourceMCP || item.Direction != DirectionUpload || item.BatchID != 0 {
		return Record{}, 0, ErrNotFound
	}
	return item, requestID, validateMCPUploadRuntime(ctx, s.db, item.RuntimeID, targetRef)
}

type uploadRuntimeReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validateMCPUploadRuntime(ctx context.Context, database uploadRuntimeReader, runtimeID int64, targetRef string) error {
	kind, targetID, profileID, valid := connectors.ParseTargetRef(targetRef)
	if !valid || runtimeID < 1 {
		return ErrNotFound
	}
	var found int
	err := database.QueryRowContext(ctx, `SELECT 1 FROM connector_runtime_surfaces
		WHERE id = ? AND connector_kind = ? AND target_id = ? AND profile_id = ?
		AND capability_kind = 'file_transfer' AND status = 'active'`, runtimeID, kind, targetID, profileID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) ExpireMCPUploads(ctx context.Context, now time.Time) error {
	cutoff := now.UTC().Format(mcpUploadTimeLayout)
	const disposableStage = `EXISTS (SELECT 1 FROM file_transfer_start_idempotency i
		WHERE i.resource_id = f.id AND i.resource_kind = 'transfer' AND i.scope LIKE 'mcp-upload:%'
		AND (f.temp_expires_at <= ? OR EXISTS (
			SELECT 1 FROM connector_action_requests a
			WHERE a.token_id = CAST(substr(i.scope, length('mcp-upload:') + 1) AS INTEGER)
			AND a.source = 'mcp' AND a.idempotency_key = i.idempotency_key
			AND a.status IN ('declined', 'stale', 'blocked', 'failed', 'error', 'canceled')
		)))`
	rows, err := s.db.QueryContext(ctx, `SELECT f.id FROM file_transfers f
		WHERE f.source = ? AND f.direction = ? AND f.status = ? AND `+disposableStage+`
		ORDER BY f.id LIMIT ?`, SourceMCP, DirectionUpload, StatusPending, cutoff, MaxMCPUploadStagedFiles)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	for _, id := range ids {
		_, err := s.updateTransferWithHistory(ctx, id, "expire MCP upload", `UPDATE file_transfers AS f
			SET status = ?, error = ?, completed_at = ?, updated_at = ?, temp_expires_at = ?
			WHERE f.id = ? AND f.status = ? AND `+disposableStage, StatusCanceled,
			"MCP upload canceled before dispatch", cutoff, cutoff, cutoff, id, StatusPending, cutoff)
		if err != nil {
			return err
		}
	}
	return nil
}
