package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	DirectionUpload   = "upload"
	DirectionDownload = "download"

	SourceUI  = "ui"
	SourceMCP = "mcp"

	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusPaused    = "paused"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
)

const maxPathRunes = 4096

var ErrNotFound = errors.New("file transfer not found")

type Record struct {
	ID               int64  `json:"id"`
	BatchID          int64  `json:"batch_id"`
	QueueIndex       int    `json:"queue_index"`
	ServerID         int64  `json:"server_id"`
	ServerName       string `json:"server_name"`
	Direction        string `json:"direction"`
	Source           string `json:"source"`
	Status           string `json:"status"`
	LocalPath        string `json:"local_path"`
	RemotePath       string `json:"remote_path"`
	FileName         string `json:"file_name"`
	SizeBytes        int64  `json:"size_bytes"`
	TransferredBytes int64  `json:"transferred_bytes"`
	BytesPerSecond   int64  `json:"bytes_per_second"`
	ETASeconds       int64  `json:"eta_seconds"`
	ChecksumSHA256   string `json:"checksum_sha256"`
	Error            string `json:"error"`
	CreatedAt        string `json:"created_at"`
	StartedAt        string `json:"started_at,omitempty"`
	CompletedAt      string `json:"completed_at,omitempty"`
	UpdatedAt        string `json:"updated_at"`

	TempPath string `json:"-"`
}

type CreateRequest struct {
	BatchID          int64
	QueueIndex       int
	ServerID         int64
	Direction        string
	Source           string
	LocalPath        string
	RemotePath       string
	FileName         string
	SizeBytes        int64
	TransferredBytes int64
	TempPath         string
}

type BatchRecord struct {
	ID               int64    `json:"id"`
	ServerID         int64    `json:"server_id"`
	ServerName       string   `json:"server_name"`
	Direction        string   `json:"direction"`
	Source           string   `json:"source"`
	Status           string   `json:"status"`
	ArchiveName      string   `json:"archive_name"`
	TotalItems       int      `json:"total_items"`
	CompletedItems   int      `json:"completed_items"`
	FailedItems      int      `json:"failed_items"`
	CanceledItems    int      `json:"canceled_items"`
	SizeBytes        int64    `json:"size_bytes"`
	TransferredBytes int64    `json:"transferred_bytes"`
	BytesPerSecond   int64    `json:"bytes_per_second"`
	ETASeconds       int64    `json:"eta_seconds"`
	Error            string   `json:"error"`
	CreatedAt        string   `json:"created_at"`
	StartedAt        string   `json:"started_at,omitempty"`
	CompletedAt      string   `json:"completed_at,omitempty"`
	UpdatedAt        string   `json:"updated_at"`
	Items            []Record `json:"items,omitempty"`

	ArchivePath string `json:"-"`
}

type CreateBatchRequest struct {
	ServerID    int64
	Direction   string
	Source      string
	ArchiveName string
	Items       []CreateRequest
}

type ListFilter struct {
	Direction string
	Status    string
	ServerID  int64
	Query     string
	Limit     int
	Offset    int
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, request CreateRequest) (Record, error) {
	normalized, err := normalizeCreateRequest(request)
	if err != nil {
		return Record{}, err
	}
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO file_transfers (
			batch_id, queue_index, server_id, direction, source, status, local_path, remote_path, file_name,
			size_bytes, transferred_bytes, temp_path, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableBatchID(normalized.BatchID),
		normalized.QueueIndex,
		normalized.ServerID,
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
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id int64) (Record, error) {
	var item Record
	err := s.db.QueryRowContext(ctx, `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.server_id, COALESCE(srv.name, ''), ft.direction, ft.source, ft.status,
			ft.local_path, ft.remote_path, ft.file_name, ft.size_bytes, ft.transferred_bytes,
			ft.bytes_per_second, ft.eta_seconds, ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN servers srv ON srv.id = ft.server_id
		WHERE ft.id = ?`,
		id,
	).Scan(
		&item.ID,
		&item.BatchID,
		&item.QueueIndex,
		&item.ServerID,
		&item.ServerName,
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
	countQuery := `SELECT COUNT(*) FROM file_transfers ft LEFT JOIN servers srv ON srv.id = ft.server_id` + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count file transfers: %w", err)
	}

	query := `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.server_id, COALESCE(srv.name, ''), ft.direction, ft.source, ft.status,
			ft.local_path, ft.remote_path, ft.file_name, ft.size_bytes, ft.transferred_bytes,
			ft.bytes_per_second, ft.eta_seconds, ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN servers srv ON srv.id = ft.server_id` + where + `
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
			&item.ServerID,
			&item.ServerName,
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
			server_id, direction, source, status, archive_name, total_items,
			size_bytes, eta_seconds, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.ServerID,
		normalized.Direction,
		normalized.Source,
		StatusPending,
		normalized.ArchiveName,
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
				batch_id, queue_index, server_id, direction, source, status, local_path,
				remote_path, file_name, size_bytes, transferred_bytes, temp_path, eta_seconds,
				created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			batchID,
			i,
			item.ServerID,
			item.Direction,
			item.Source,
			StatusPending,
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
	return s.GetBatch(ctx, batchID)
}

func (s *Store) GetBatch(ctx context.Context, id int64) (BatchRecord, error) {
	var item BatchRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT b.id, b.server_id, COALESCE(srv.name, ''), b.direction, b.source, b.status,
			b.archive_name, b.archive_path, b.total_items, b.completed_items, b.failed_items,
			b.canceled_items, b.size_bytes, b.transferred_bytes, b.bytes_per_second,
			b.eta_seconds, b.error, b.created_at, COALESCE(b.started_at, ''),
			COALESCE(b.completed_at, ''), b.updated_at
		FROM file_transfer_batches b
		LEFT JOIN servers srv ON srv.id = b.server_id
		WHERE b.id = ?`,
		id,
	).Scan(
		&item.ID,
		&item.ServerID,
		&item.ServerName,
		&item.Direction,
		&item.Source,
		&item.Status,
		&item.ArchiveName,
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
	items, err := s.ListBatchItems(ctx, id)
	if err != nil {
		return BatchRecord{}, err
	}
	item.Items = items
	return item, nil
}

func (s *Store) ListBatchItems(ctx context.Context, batchID int64) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.server_id, COALESCE(srv.name, ''),
			ft.direction, ft.source, ft.status, ft.local_path, ft.remote_path, ft.file_name,
			ft.size_bytes, ft.transferred_bytes, ft.bytes_per_second, ft.eta_seconds,
			ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN servers srv ON srv.id = ft.server_id
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
			&item.ServerID,
			&item.ServerName,
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

func (s *Store) MarkRunning(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		StatusRunning,
		now,
		now,
		id,
		StatusPending,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("mark file transfer running: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read file transfer running rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) MarkBatchRunning(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		StatusRunning,
		now,
		now,
		id,
		StatusPending,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("mark file transfer batch running: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read file transfer batch running rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) UpdateProgress(ctx context.Context, id int64, transferred int64, size int64) error {
	return s.UpdateProgressStats(ctx, id, transferred, size, 0, -1)
}

func (s *Store) UpdateProgressStats(ctx context.Context, id int64, transferred int64, size int64, bytesPerSecond int64, etaSeconds int64) error {
	if transferred < 0 {
		transferred = 0
	}
	if size < 0 {
		size = 0
	}
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET transferred_bytes = ?, size_bytes = CASE WHEN ? > 0 THEN ? ELSE size_bytes END,
			bytes_per_second = ?, eta_seconds = ?, updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		transferred,
		size,
		size,
		bytesPerSecond,
		etaSeconds,
		now,
		id,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return fmt.Errorf("update file transfer progress: %w", err)
	}
	return nil
}

func (s *Store) Complete(ctx context.Context, id int64, transferred int64, checksum string) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, transferred_bytes = CASE WHEN ? >= 0 THEN ? ELSE transferred_bytes END,
			checksum_sha256 = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		StatusCompleted,
		transferred,
		transferred,
		strings.TrimSpace(checksum),
		now,
		now,
		id,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("complete file transfer: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read completed file transfer rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) Fail(ctx context.Context, id int64, errorText string) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
		StatusFailed,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPending,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("fail file transfer: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read failed file transfer rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) Cancel(ctx context.Context, id int64, errorText string) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
		StatusCanceled,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPending,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("cancel file transfer: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read canceled file transfer rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) Pause(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		StatusPaused,
		now,
		id,
		StatusRunning,
	)
	if err != nil {
		return false, fmt.Errorf("pause file transfer: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read paused file transfer rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) PauseBatch(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		StatusPaused,
		now,
		id,
		StatusRunning,
	)
	if err != nil {
		return false, fmt.Errorf("pause file transfer batch: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, updated_at = ?
		WHERE batch_id = ? AND status = ?`,
		StatusPaused,
		now,
		id,
		StatusRunning,
	); err != nil {
		return false, fmt.Errorf("pause file transfer batch items: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read paused file transfer batch rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) ResumeBatch(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		StatusRunning,
		now,
		id,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("resume file transfer batch: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, updated_at = ?
		WHERE batch_id = ? AND status = ?`,
		StatusRunning,
		now,
		id,
		StatusPaused,
	); err != nil {
		return false, fmt.Errorf("resume file transfer batch items: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read resumed file transfer batch rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) CancelBatch(ctx context.Context, id int64, errorText string) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
		StatusCanceled,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPending,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("cancel file transfer batch: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE batch_id = ? AND status IN (?, ?, ?)`,
		StatusCanceled,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPending,
		StatusRunning,
		StatusPaused,
	); err != nil {
		return false, fmt.Errorf("cancel file transfer batch items: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read canceled file transfer batch rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) RecalculateBatch(ctx context.Context, id int64) error {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET
			total_items = (SELECT COUNT(*) FROM file_transfers WHERE batch_id = ?),
			completed_items = (SELECT COUNT(*) FROM file_transfers WHERE batch_id = ? AND status = ?),
			failed_items = (SELECT COUNT(*) FROM file_transfers WHERE batch_id = ? AND status = ?),
			canceled_items = (SELECT COUNT(*) FROM file_transfers WHERE batch_id = ? AND status = ?),
			size_bytes = COALESCE((SELECT SUM(size_bytes) FROM file_transfers WHERE batch_id = ?), 0),
			transferred_bytes = COALESCE((SELECT SUM(transferred_bytes) FROM file_transfers WHERE batch_id = ?), 0),
			bytes_per_second = COALESCE((SELECT SUM(bytes_per_second) FROM file_transfers WHERE batch_id = ? AND status IN (?, ?)), 0),
			eta_seconds = CASE
				WHEN COALESCE((SELECT SUM(bytes_per_second) FROM file_transfers WHERE batch_id = ? AND status IN (?, ?)), 0) > 0
				THEN CAST((
					COALESCE((SELECT SUM(size_bytes) FROM file_transfers WHERE batch_id = ?), 0) -
					COALESCE((SELECT SUM(transferred_bytes) FROM file_transfers WHERE batch_id = ?), 0)
				) / COALESCE((SELECT SUM(bytes_per_second) FROM file_transfers WHERE batch_id = ? AND status IN (?, ?)), 1) AS INTEGER)
				ELSE -1
			END,
			updated_at = ?
		WHERE id = ?`,
		id,
		id, StatusCompleted,
		id, StatusFailed,
		id, StatusCanceled,
		id,
		id,
		id, StatusRunning, StatusPaused,
		id, StatusRunning, StatusPaused,
		id,
		id,
		id, StatusRunning, StatusPaused,
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("recalculate file transfer batch: %w", err)
	}
	return nil
}

func (s *Store) CompleteBatch(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = CASE
				WHEN failed_items > 0 THEN ?
				WHEN canceled_items > 0 THEN ?
				ELSE ?
			END,
			completed_at = COALESCE(completed_at, ?),
			bytes_per_second = 0,
			eta_seconds = 0,
			updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		StatusFailed,
		StatusCanceled,
		StatusCompleted,
		now,
		now,
		id,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("complete file transfer batch: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read completed file transfer batch rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) SetBatchArchive(ctx context.Context, id int64, archivePath string) error {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET archive_path = ?, updated_at = ?
		WHERE id = ?`,
		strings.TrimSpace(archivePath),
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("set file transfer batch archive: %w", err)
	}
	return nil
}

func listWhere(filter ListFilter) (string, []any) {
	clauses := []string{}
	args := []any{}
	if filter.Direction != "" {
		clauses = append(clauses, "ft.direction = ?")
		args = append(args, filter.Direction)
	}
	if filter.Status != "" {
		clauses = append(clauses, "ft.status = ?")
		args = append(args, filter.Status)
	}
	if filter.ServerID > 0 {
		clauses = append(clauses, "ft.server_id = ?")
		args = append(args, filter.ServerID)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		clauses = append(clauses, "(ft.remote_path LIKE ? OR ft.local_path LIKE ? OR ft.file_name LIKE ? OR COALESCE(srv.name, '') LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func normalizeCreateRequest(request CreateRequest) (CreateRequest, error) {
	request.Direction = strings.TrimSpace(request.Direction)
	request.Source = strings.TrimSpace(request.Source)
	request.LocalPath = strings.TrimSpace(request.LocalPath)
	request.RemotePath = strings.TrimSpace(request.RemotePath)
	request.FileName = strings.TrimSpace(request.FileName)
	request.TempPath = strings.TrimSpace(request.TempPath)
	if request.Source == "" {
		request.Source = SourceUI
	}
	if request.ServerID < 1 {
		return request, fmt.Errorf("server_id is required")
	}
	if request.Direction != DirectionUpload && request.Direction != DirectionDownload {
		return request, fmt.Errorf("direction must be upload or download")
	}
	if request.Source != SourceUI && request.Source != SourceMCP {
		return request, fmt.Errorf("source must be ui or mcp")
	}
	if request.BatchID < 0 {
		return request, fmt.Errorf("batch_id cannot be negative")
	}
	if request.QueueIndex < 0 {
		return request, fmt.Errorf("queue_index cannot be negative")
	}
	if err := validatePathLike("local_path", request.LocalPath, false); err != nil {
		return request, err
	}
	if err := validatePathLike("remote_path", request.RemotePath, true); err != nil {
		return request, err
	}
	if err := validatePathLike("file_name", request.FileName, false); err != nil {
		return request, err
	}
	if request.SizeBytes < 0 {
		return request, fmt.Errorf("size_bytes cannot be negative")
	}
	if request.TransferredBytes < 0 {
		return request, fmt.Errorf("transferred_bytes cannot be negative")
	}
	return request, nil
}

func normalizeBatchCreateRequest(request CreateBatchRequest) (CreateBatchRequest, error) {
	request.Direction = strings.TrimSpace(request.Direction)
	request.Source = strings.TrimSpace(request.Source)
	request.ArchiveName = strings.TrimSpace(request.ArchiveName)
	if request.Source == "" {
		request.Source = SourceUI
	}
	if request.ServerID < 1 {
		return request, fmt.Errorf("server_id is required")
	}
	if request.Direction != DirectionUpload && request.Direction != DirectionDownload {
		return request, fmt.Errorf("direction must be upload or download")
	}
	if request.Source != SourceUI && request.Source != SourceMCP {
		return request, fmt.Errorf("source must be ui or mcp")
	}
	if len(request.Items) == 0 {
		return request, fmt.Errorf("at least one file transfer item is required")
	}
	if len(request.Items) > 100 {
		return request, fmt.Errorf("file transfer batch cannot contain more than 100 items")
	}
	if err := validatePathLike("archive_name", request.ArchiveName, false); err != nil {
		return request, err
	}
	for i := range request.Items {
		request.Items[i].BatchID = 0
		request.Items[i].QueueIndex = i
		request.Items[i].ServerID = request.ServerID
		request.Items[i].Direction = request.Direction
		request.Items[i].Source = request.Source
		normalized, err := normalizeCreateRequest(request.Items[i])
		if err != nil {
			return request, fmt.Errorf("item %d: %w", i+1, err)
		}
		request.Items[i] = normalized
	}
	return request, nil
}

func normalizeListFilter(filter ListFilter) ListFilter {
	filter.Direction = strings.TrimSpace(filter.Direction)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	switch filter.Direction {
	case DirectionUpload, DirectionDownload:
	default:
		filter.Direction = ""
	}
	switch filter.Status {
	case StatusPending, StatusRunning, StatusPaused, StatusCompleted, StatusFailed, StatusCanceled:
	default:
		filter.Status = ""
	}
	return filter
}

func validatePathLike(field string, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if len([]rune(value)) > maxPathRunes {
		return fmt.Errorf("%s must be %d characters or fewer", field, maxPathRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s cannot contain control characters", field)
		}
	}
	return nil
}

func nullableBatchID(id int64) any {
	if id < 1 {
		return nil
	}
	return id
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}
