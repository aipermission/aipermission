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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, fmt.Errorf("begin file transfer: %w", err)
	}
	defer tx.Rollback()
	id, err := insertTransfer(ctx, tx, normalized, StatusPending, nowString())
	if err != nil {
		return Record{}, err
	}
	if err := syncTransferHistoryWithExecutor(ctx, tx, id); err != nil {
		return Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return Record{}, fmt.Errorf("commit file transfer: %w", err)
	}
	return s.createdTransfer(ctx, id)
}

func (s *Store) CreateIdempotent(ctx context.Context, request CreateRequest, claim IdempotencyClaim) (Record, bool, error) {
	normalized, err := normalizeCreateRequest(request)
	if err != nil {
		return Record{}, false, err
	}
	claim, err = normalizeIdempotencyClaim(claim, IdempotencyResourceTransfer)
	if err != nil {
		return Record{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, false, fmt.Errorf("begin idempotent file transfer: %w", err)
	}
	defer tx.Rollback()
	record, created, err := claimIdempotencyKey(ctx, tx, claim)
	if err != nil {
		return Record{}, false, err
	}
	if !created {
		_ = tx.Rollback()
		item, getErr := s.createdTransfer(ctx, record.ResourceID)
		if errors.Is(getErr, ErrNotFound) {
			return Record{}, false, ErrIdempotencyResultExpired
		}
		return item, false, getErr
	}
	id, err := insertTransfer(ctx, tx, normalized, StatusPending, nowString())
	if err != nil {
		return Record{}, false, err
	}
	if err := syncTransferHistoryWithExecutor(ctx, tx, id); err != nil {
		return Record{}, false, err
	}
	if err := completeIdempotencyClaim(ctx, tx, claim, id); err != nil {
		return Record{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Record{}, false, fmt.Errorf("commit idempotent file transfer: %w", err)
	}
	item, err := s.createdTransfer(ctx, id)
	return item, true, err
}

func insertTransfer(ctx context.Context, tx *sql.Tx, request CreateRequest, status, now string) (int64, error) {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO file_transfers (
			batch_id, queue_index, runtime_id, direction, source, status, local_path, remote_path, file_name,
			size_bytes, transferred_bytes, checksum_sha256, temp_path, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableBatchID(request.BatchID),
		request.QueueIndex,
		request.RuntimeID,
		request.Direction,
		request.Source,
		status,
		request.LocalPath,
		request.RemotePath,
		request.FileName,
		request.SizeBytes,
		request.TransferredBytes,
		request.ChecksumSHA256,
		request.TempPath,
		now,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("create file transfer: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read file transfer id: %w", err)
	}
	return id, nil
}

func (s *Store) createdTransfer(ctx context.Context, id int64) (Record, error) {
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id int64) (Record, error) {
	item, err := scanTransfer(s.db.QueryRowContext(ctx, transferSelect+` WHERE ft.id = ?`, id))
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

	query := transferSelect + where + `
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
		item, err := scanTransfer(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan file transfer: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate file transfers: %w", err)
	}
	return items, total, nil
}
