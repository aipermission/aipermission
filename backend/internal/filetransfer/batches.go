package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) ListBatches(ctx context.Context, filter BatchListFilter) ([]BatchRecord, int, error) {
	filter = normalizeBatchListFilter(filter)
	where, args := batchListWhere(filter)
	countQuery := `SELECT COUNT(*) FROM file_transfer_batches b LEFT JOIN connector_runtime_surfaces rs ON rs.id = b.runtime_id LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind` + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count file transfer batches: %w", err)
	}

	query := `
		SELECT b.id, b.runtime_id, COALESCE(ct.name, ''), b.direction, b.source, b.status,
			b.archive_name, COALESCE(b.approval_note, ''), COALESCE(b.overwrite, 0), b.archive_path, b.total_items, b.completed_items, b.failed_items,
			b.canceled_items, b.size_bytes, b.transferred_bytes, b.bytes_per_second,
			b.eta_seconds, b.error, b.created_at, COALESCE(b.started_at, ''),
			COALESCE(b.completed_at, ''), b.updated_at
		FROM file_transfer_batches b
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = b.runtime_id
		LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind` + where + `
		ORDER BY b.created_at DESC, b.id DESC
		LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list file transfer batches: %w", err)
	}
	defer rows.Close()

	items := []BatchRecord{}
	for rows.Next() {
		var item BatchRecord
		var overwrite int
		if err := rows.Scan(
			&item.ID,
			&item.RuntimeID,
			&item.TargetName,
			&item.Direction,
			&item.Source,
			&item.Status,
			&item.ArchiveName,
			&item.ApprovalNote,
			&overwrite,
			&item.ArchivePath,
			&item.TotalItems,
			&item.CompletedItems,
			&item.FailedItems,
			&item.CanceledItems,
			&item.SizeBytes,
			&item.TransferredBytes,
			&item.BytesPerSecond,
			&item.ETASeconds,
			&item.Error,
			&item.CreatedAt,
			&item.StartedAt,
			&item.CompletedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan file transfer batch: %w", err)
		}
		item.Overwrite = overwrite != 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate file transfer batches: %w", err)
	}
	return items, total, nil
}

func (s *Store) CreateBatch(ctx context.Context, request CreateBatchRequest) (BatchRecord, error) {
	normalized, err := normalizeBatchCreateRequest(request)
	if err != nil {
		return BatchRecord{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BatchRecord{}, fmt.Errorf("begin file transfer batch: %w", err)
	}
	defer tx.Rollback()

	now := nowString()
	var totalSize int64
	for _, item := range normalized.Items {
		totalSize += item.SizeBytes
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO file_transfer_batches (
			runtime_id, direction, source, status, archive_name, approval_note, overwrite, total_items,
			size_bytes, eta_seconds, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.RuntimeID,
		normalized.Direction,
		normalized.Source,
		normalized.Status,
		normalized.ArchiveName,
		normalized.ApprovalNote,
		boolInt(normalized.Overwrite),
		len(normalized.Items),
		totalSize,
		-1,
		now,
		now,
	)
	if err != nil {
		return BatchRecord{}, fmt.Errorf("create file transfer batch: %w", err)
	}
	batchID, err := result.LastInsertId()
	if err != nil {
		return BatchRecord{}, fmt.Errorf("read file transfer batch id: %w", err)
	}
	for i, item := range normalized.Items {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO file_transfers (
				batch_id, queue_index, runtime_id, direction, source, status, local_path,
				remote_path, file_name, size_bytes, transferred_bytes, temp_path, eta_seconds,
				created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			batchID,
			i,
			item.RuntimeID,
			item.Direction,
			item.Source,
			normalized.Status,
			item.LocalPath,
			item.RemotePath,
			item.FileName,
			item.SizeBytes,
			item.TransferredBytes,
			item.TempPath,
			-1,
			now,
			now,
		)
		if err != nil {
			return BatchRecord{}, fmt.Errorf("create file transfer batch item: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return BatchRecord{}, fmt.Errorf("commit file transfer batch: %w", err)
	}
	batch, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return BatchRecord{}, err
	}
	if err := s.syncBatchTransferHistory(ctx, batchID); err != nil {
		return BatchRecord{}, err
	}
	return batch, nil
}

func (s *Store) GetBatch(ctx context.Context, id int64) (BatchRecord, error) {
	var item BatchRecord
	var overwrite int
	err := s.db.QueryRowContext(ctx, `
		SELECT b.id, b.runtime_id, COALESCE(ct.name, ''), b.direction, b.source, b.status,
			b.archive_name, COALESCE(b.approval_note, ''), COALESCE(b.overwrite, 0), b.archive_path, b.total_items, b.completed_items, b.failed_items,
			b.canceled_items, b.size_bytes, b.transferred_bytes, b.bytes_per_second,
			b.eta_seconds, b.error, b.created_at, COALESCE(b.started_at, ''),
			COALESCE(b.completed_at, ''), b.updated_at
		FROM file_transfer_batches b
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = b.runtime_id
		LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		WHERE b.id = ?`,
		id,
	).Scan(
		&item.ID,
		&item.RuntimeID,
		&item.TargetName,
		&item.Direction,
		&item.Source,
		&item.Status,
		&item.ArchiveName,
		&item.ApprovalNote,
		&overwrite,
		&item.ArchivePath,
		&item.TotalItems,
		&item.CompletedItems,
		&item.FailedItems,
		&item.CanceledItems,
		&item.SizeBytes,
		&item.TransferredBytes,
		&item.BytesPerSecond,
		&item.ETASeconds,
		&item.Error,
		&item.CreatedAt,
		&item.StartedAt,
		&item.CompletedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BatchRecord{}, ErrNotFound
	}
	if err != nil {
		return BatchRecord{}, fmt.Errorf("get file transfer batch: %w", err)
	}
	item.Overwrite = overwrite != 0
	items, err := s.ListBatchItems(ctx, id)
	if err != nil {
		return BatchRecord{}, err
	}
	item.Items = items
	return item, nil
}

func (s *Store) ListBatchItems(ctx context.Context, batchID int64) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.runtime_id, COALESCE(ct.name, ''),
			ft.direction, ft.source, ft.status, ft.local_path, ft.remote_path, ft.file_name,
			ft.size_bytes, ft.transferred_bytes, ft.bytes_per_second, ft.eta_seconds,
			ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = ft.runtime_id
		LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		WHERE ft.batch_id = ?
		ORDER BY ft.queue_index ASC, ft.id ASC`,
		batchID,
	)
	if err != nil {
		return nil, fmt.Errorf("list file transfer batch items: %w", err)
	}
	defer rows.Close()
	var items []Record
	for rows.Next() {
		var item Record
		if err := rows.Scan(
			&item.ID,
			&item.BatchID,
			&item.QueueIndex,
			&item.RuntimeID,
			&item.TargetName,
			&item.Direction,
			&item.Source,
			&item.Status,
			&item.LocalPath,
			&item.RemotePath,
			&item.FileName,
			&item.SizeBytes,
			&item.TransferredBytes,
			&item.BytesPerSecond,
			&item.ETASeconds,
			&item.ChecksumSHA256,
			&item.TempPath,
			&item.Error,
			&item.CreatedAt,
			&item.StartedAt,
			&item.CompletedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan file transfer batch item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file transfer batch items: %w", err)
	}
	return items, nil
}

func (s *Store) NextBatchPendingItem(ctx context.Context, batchID int64) (Record, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.runtime_id, COALESCE(ct.name, ''),
			ft.direction, ft.source, ft.status, ft.local_path, ft.remote_path, ft.file_name,
			ft.size_bytes, ft.transferred_bytes, ft.bytes_per_second, ft.eta_seconds,
			ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = ft.runtime_id
		LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		WHERE ft.batch_id = ? AND ft.status = ?
		ORDER BY ft.queue_index ASC, ft.id ASC
		LIMIT 1`,
		batchID,
		StatusPending,
	)
	var item Record
	if err := row.Scan(
		&item.ID,
		&item.BatchID,
		&item.QueueIndex,
		&item.RuntimeID,
		&item.TargetName,
		&item.Direction,
		&item.Source,
		&item.Status,
		&item.LocalPath,
		&item.RemotePath,
		&item.FileName,
		&item.SizeBytes,
		&item.TransferredBytes,
		&item.BytesPerSecond,
		&item.ETASeconds,
		&item.ChecksumSHA256,
		&item.TempPath,
		&item.Error,
		&item.CreatedAt,
		&item.StartedAt,
		&item.CompletedAt,
		&item.UpdatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	} else if err != nil {
		return Record{}, fmt.Errorf("get next pending file transfer batch item: %w", err)
	}
	return item, nil
}
