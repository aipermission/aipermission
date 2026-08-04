package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) Create(ctx context.Context, request CreateRequest) (Record, error) {
	normalized, err := normalizeCreateRequest(request)
	if err != nil {
		return Record{}, err
	}
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO file_transfers (
			batch_id, queue_index, runtime_id, direction, source, status, local_path, remote_path, file_name,
			size_bytes, transferred_bytes, temp_path, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableBatchID(normalized.BatchID),
		normalized.QueueIndex,
		normalized.RuntimeID,
		normalized.Direction,
		normalized.Source,
		StatusPending,
		normalized.LocalPath,
		normalized.RemotePath,
		normalized.FileName,
		normalized.SizeBytes,
		normalized.TransferredBytes,
		normalized.TempPath,
		now,
		now,
	)
	if err != nil {
		return Record{}, fmt.Errorf("create file transfer: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Record{}, fmt.Errorf("read file transfer id: %w", err)
	}
	item, err := s.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if err := s.syncTransferHistory(ctx, id); err != nil {
		return Record{}, err
	}
	return item, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Record, error) {
	var item Record
	err := s.db.QueryRowContext(ctx, `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.runtime_id, COALESCE(ct.name, ''), ft.direction, ft.source, ft.status,
			ft.local_path, ft.remote_path, ft.file_name, ft.size_bytes, ft.transferred_bytes,
			ft.bytes_per_second, ft.eta_seconds, ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = ft.runtime_id
		LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		WHERE ft.id = ?`,
		id,
	).Scan(
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
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("get file transfer: %w", err)
	}
	return item, nil
}

func (s *Store) List(ctx context.Context, filter ListFilter) ([]Record, int, error) {
	filter = normalizeListFilter(filter)
	where, args := listWhere(filter)
	countQuery := `SELECT COUNT(*) FROM file_transfers ft LEFT JOIN connector_runtime_surfaces rs ON rs.id = ft.runtime_id LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind` + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count file transfers: %w", err)
	}

	query := `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.runtime_id, COALESCE(ct.name, ''), ft.direction, ft.source, ft.status,
			ft.local_path, ft.remote_path, ft.file_name, ft.size_bytes, ft.transferred_bytes,
			ft.bytes_per_second, ft.eta_seconds, ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = ft.runtime_id
		LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind` + where + `
		ORDER BY ft.created_at DESC, ft.id DESC
		LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list file transfers: %w", err)
	}
	defer rows.Close()

	items := []Record{}
	for rows.Next() {
		var item Record
		var batchID int64
		var queueIndex int
		if err := rows.Scan(
			&item.ID,
			&batchID,
			&queueIndex,
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
			return nil, 0, fmt.Errorf("scan file transfer: %w", err)
		}
		item.BatchID = batchID
		item.QueueIndex = queueIndex
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate file transfers: %w", err)
	}
	return items, total, nil
}
