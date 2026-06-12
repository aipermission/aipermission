package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/history"
)

const (
	DirectionUpload   = "upload"
	DirectionDownload = "download"

	SourceUI  = "ui"
	SourceMCP = "mcp"

	StatusPending         = "pending"
	StatusPendingApproval = "pending_approval"
	StatusRunning         = "running"
	StatusPaused          = "paused"
	StatusCompleted       = "completed"
	StatusFailed          = "failed"
	StatusCanceled        = "canceled"
)

const maxPathRunes = 4096

var ErrNotFound = errors.New("file transfer not found")
var ErrInvalidState = errors.New("file transfer invalid state")
var ErrInvalidArgument = errors.New("file transfer invalid argument")

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
	ApprovalNote     string   `json:"approval_note"`
	Overwrite        bool     `json:"overwrite"`
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
	ServerID     int64
	Direction    string
	Source       string
	Status       string
	ApprovalNote string
	Overwrite    bool
	ArchiveName  string
	Items        []CreateRequest
}

type BatchApprovalRequest struct {
	ApprovedItemIDs []int64
	Note            string
}

type ListFilter struct {
	Direction string
	Status    string
	ServerID  int64
	TargetIDs []int64
	Query     string
	Limit     int
	Offset    int
}

type BatchListFilter struct {
	Direction string
	Status    string
	ServerID  int64
	TargetIDs []int64
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

func (s *Store) syncTransferHistory(ctx context.Context, id int64) error {
	if id < 1 {
		return nil
	}
	return history.NewStore(s.db).SyncFileTransfer(ctx, id)
}

func (s *Store) syncBatchTransferHistory(ctx context.Context, batchID int64) error {
	if batchID < 1 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM file_transfers WHERE batch_id = ?`, batchID)
	if err != nil {
		return fmt.Errorf("read batch transfer ids for history sync: %w", err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan batch transfer id for history sync: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate batch transfer ids for history sync: %w", err)
	}
	return s.syncTransferHistoryIDs(ctx, ids)
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
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.server_id, COALESCE(ct.name, ''), ft.direction, ft.source, ft.status,
			ft.local_path, ft.remote_path, ft.file_name, ft.size_bytes, ft.transferred_bytes,
			ft.bytes_per_second, ft.eta_seconds, ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN connector_credential_profiles cp ON cp.id = ft.server_id AND cp.connector_kind = 'ssh'
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh'
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
	countQuery := `SELECT COUNT(*) FROM file_transfers ft LEFT JOIN connector_credential_profiles cp ON cp.id = ft.server_id AND cp.connector_kind = 'ssh' LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh'` + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count file transfers: %w", err)
	}

	query := `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.server_id, COALESCE(ct.name, ''), ft.direction, ft.source, ft.status,
			ft.local_path, ft.remote_path, ft.file_name, ft.size_bytes, ft.transferred_bytes,
			ft.bytes_per_second, ft.eta_seconds, ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN connector_credential_profiles cp ON cp.id = ft.server_id AND cp.connector_kind = 'ssh'
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh'` + where + `
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

func (s *Store) ListBatches(ctx context.Context, filter BatchListFilter) ([]BatchRecord, int, error) {
	filter = normalizeBatchListFilter(filter)
	where, args := batchListWhere(filter)
	countQuery := `SELECT COUNT(*) FROM file_transfer_batches b LEFT JOIN connector_credential_profiles cp ON cp.id = b.server_id AND cp.connector_kind = 'ssh' LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh'` + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count file transfer batches: %w", err)
	}

	query := `
		SELECT b.id, b.server_id, COALESCE(ct.name, ''), b.direction, b.source, b.status,
			b.archive_name, COALESCE(b.approval_note, ''), COALESCE(b.overwrite, 0), b.archive_path, b.total_items, b.completed_items, b.failed_items,
			b.canceled_items, b.size_bytes, b.transferred_bytes, b.bytes_per_second,
			b.eta_seconds, b.error, b.created_at, COALESCE(b.started_at, ''),
			COALESCE(b.completed_at, ''), b.updated_at
		FROM file_transfer_batches b
		LEFT JOIN connector_credential_profiles cp ON cp.id = b.server_id AND cp.connector_kind = 'ssh'
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh'` + where + `
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
			&item.ServerID,
			&item.ServerName,
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
			server_id, direction, source, status, archive_name, approval_note, overwrite, total_items,
			size_bytes, eta_seconds, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.ServerID,
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
		SELECT b.id, b.server_id, COALESCE(ct.name, ''), b.direction, b.source, b.status,
			b.archive_name, COALESCE(b.approval_note, ''), COALESCE(b.overwrite, 0), b.archive_path, b.total_items, b.completed_items, b.failed_items,
			b.canceled_items, b.size_bytes, b.transferred_bytes, b.bytes_per_second,
			b.eta_seconds, b.error, b.created_at, COALESCE(b.started_at, ''),
			COALESCE(b.completed_at, ''), b.updated_at
		FROM file_transfer_batches b
		LEFT JOIN connector_credential_profiles cp ON cp.id = b.server_id AND cp.connector_kind = 'ssh'
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh'
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
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.server_id, COALESCE(ct.name, ''),
			ft.direction, ft.source, ft.status, ft.local_path, ft.remote_path, ft.file_name,
			ft.size_bytes, ft.transferred_bytes, ft.bytes_per_second, ft.eta_seconds,
			ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN connector_credential_profiles cp ON cp.id = ft.server_id AND cp.connector_kind = 'ssh'
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh'
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

func (s *Store) NextBatchPendingItem(ctx context.Context, batchID int64) (Record, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.server_id, COALESCE(ct.name, ''),
			ft.direction, ft.source, ft.status, ft.local_path, ft.remote_path, ft.file_name,
			ft.size_bytes, ft.transferred_bytes, ft.bytes_per_second, ft.eta_seconds,
			ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN connector_credential_profiles cp ON cp.id = ft.server_id AND cp.connector_kind = 'ssh'
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh'
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
	); errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	} else if err != nil {
		return Record{}, fmt.Errorf("get next pending file transfer batch item: %w", err)
	}
	return item, nil
}

func (s *Store) MarkRunning(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
		StatusRunning,
		now,
		now,
		id,
		StatusPending,
		StatusPendingApproval,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("mark file transfer running: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read file transfer running rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
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
	if rows > 0 {
		if err := s.syncBatchTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) ApproveBatch(ctx context.Context, id int64, request BatchApprovalRequest) (BatchRecord, []Record, error) {
	approvedIDs, err := normalizeApprovedItemIDs(request.ApprovedItemIDs)
	if err != nil {
		return BatchRecord{}, nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BatchRecord{}, nil, fmt.Errorf("begin file transfer batch approval: %w", err)
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM file_transfer_batches WHERE id = ?`, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return BatchRecord{}, nil, ErrNotFound
	} else if err != nil {
		return BatchRecord{}, nil, fmt.Errorf("read file transfer batch approval status: %w", err)
	}
	if status != StatusPendingApproval {
		return BatchRecord{}, nil, ErrInvalidState
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, temp_path
		FROM file_transfers
		WHERE batch_id = ? AND status = ?
		ORDER BY queue_index ASC, id ASC`,
		id,
		StatusPendingApproval,
	)
	if err != nil {
		return BatchRecord{}, nil, fmt.Errorf("read pending approval file transfer items: %w", err)
	}
	type pendingItem struct {
		id       int64
		tempPath string
	}
	var pending []pendingItem
	for rows.Next() {
		var item pendingItem
		if err := rows.Scan(&item.id, &item.tempPath); err != nil {
			rows.Close()
			return BatchRecord{}, nil, fmt.Errorf("scan pending approval file transfer item: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return BatchRecord{}, nil, fmt.Errorf("iterate pending approval file transfer items: %w", err)
	}
	rows.Close()
	if len(pending) == 0 {
		return BatchRecord{}, nil, ErrInvalidState
	}
	approvedSet := map[int64]bool{}
	for _, id := range approvedIDs {
		approvedSet[id] = true
	}
	foundApproved := map[int64]bool{}
	for _, item := range pending {
		if approvedSet[item.id] {
			foundApproved[item.id] = true
		}
	}
	for id := range approvedSet {
		if !foundApproved[id] {
			return BatchRecord{}, nil, ErrInvalidArgument
		}
	}

	note := strings.TrimSpace(request.Note)
	now := nowString()
	var rejected []Record
	for _, item := range pending {
		if approvedSet[item.id] {
			if _, err := tx.ExecContext(ctx, `
				UPDATE file_transfers
				SET status = ?, updated_at = ?
				WHERE id = ? AND status = ?`,
				StatusPending,
				now,
				item.id,
				StatusPendingApproval,
			); err != nil {
				return BatchRecord{}, nil, fmt.Errorf("approve file transfer item: %w", err)
			}
			continue
		}
		reason := note
		if reason == "" {
			reason = "rejected by local user"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE file_transfers
			SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE id = ? AND status = ?`,
			StatusCanceled,
			reason,
			now,
			now,
			item.id,
			StatusPendingApproval,
		); err != nil {
			return BatchRecord{}, nil, fmt.Errorf("reject file transfer item: %w", err)
		}
		rejected = append(rejected, Record{ID: item.id, TempPath: item.tempPath})
	}

	nextStatus := StatusPending
	if len(approvedIDs) == 0 {
		nextStatus = StatusCanceled
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, approval_note = ?, error = CASE WHEN ? = ? THEN ? ELSE error END,
			completed_at = CASE WHEN ? = ? THEN COALESCE(completed_at, ?) ELSE completed_at END,
			updated_at = ?
		WHERE id = ? AND status = ?`,
		nextStatus,
		note,
		nextStatus,
		StatusCanceled,
		rejectionNote(note),
		nextStatus,
		StatusCanceled,
		now,
		now,
		id,
		StatusPendingApproval,
	); err != nil {
		return BatchRecord{}, nil, fmt.Errorf("approve file transfer batch: %w", err)
	}
	if err := recalculateBatch(ctx, tx, id); err != nil {
		return BatchRecord{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return BatchRecord{}, nil, fmt.Errorf("commit file transfer batch approval: %w", err)
	}
	batch, err := s.GetBatch(ctx, id)
	if err != nil {
		return BatchRecord{}, nil, err
	}
	if err := s.syncBatchTransferHistory(ctx, id); err != nil {
		return BatchRecord{}, nil, err
	}
	return batch, rejected, nil
}

func (s *Store) DeclineBatch(ctx context.Context, id int64, note string) (BatchRecord, []Record, error) {
	batch, rejected, err := s.ApproveBatch(ctx, id, BatchApprovalRequest{ApprovedItemIDs: nil, Note: note})
	if err != nil {
		return BatchRecord{}, nil, err
	}
	return batch, rejected, nil
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
	return s.syncTransferHistory(ctx, id)
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
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
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
		StatusPendingApproval,
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
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
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
		StatusPendingApproval,
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
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
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
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
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
	if rows > 0 {
		if err := s.syncBatchTransferHistory(ctx, id); err != nil {
			return false, err
		}
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
	if rows > 0 {
		if err := s.syncBatchTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) UpdatePausedBatchQueue(ctx context.Context, id int64, orderedPendingIDs []int64) ([]Record, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin file transfer batch queue update: %w", err)
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM file_transfer_batches WHERE id = ?`, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read file transfer batch status: %w", err)
	}
	if status != StatusPaused {
		return nil, ErrInvalidState
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, queue_index, status, temp_path
		FROM file_transfers
		WHERE batch_id = ?
		ORDER BY queue_index ASC, id ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("read paused file transfer batch items: %w", err)
	}
	type queueItem struct {
		id         int64
		queueIndex int
		status     string
		tempPath   string
	}
	var items []queueItem
	for rows.Next() {
		var item queueItem
		if err := rows.Scan(&item.id, &item.queueIndex, &item.status, &item.tempPath); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan paused file transfer batch item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate paused file transfer batch items: %w", err)
	}
	rows.Close()

	pending := map[int64]queueItem{}
	maxStableIndex := -1
	for _, item := range items {
		if item.status == StatusPending {
			pending[item.id] = item
			continue
		}
		if item.queueIndex > maxStableIndex {
			maxStableIndex = item.queueIndex
		}
	}
	seen := map[int64]bool{}
	for _, itemID := range orderedPendingIDs {
		if itemID < 1 {
			return nil, ErrInvalidArgument
		}
		if seen[itemID] {
			return nil, ErrInvalidArgument
		}
		if _, ok := pending[itemID]; !ok {
			return nil, ErrInvalidState
		}
		seen[itemID] = true
	}

	now := nowString()
	var removed []Record
	for itemID, item := range pending {
		if seen[itemID] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM file_transfers
			WHERE id = ? AND status = ?`,
			itemID,
			StatusPending,
		); err != nil {
			return nil, fmt.Errorf("remove paused file transfer batch item: %w", err)
		}
		removed = append(removed, Record{ID: itemID, TempPath: item.tempPath})
	}

	for index, itemID := range orderedPendingIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE file_transfers
			SET queue_index = ?, updated_at = ?
			WHERE id = ? AND status = ?`,
			maxStableIndex+1+index,
			now,
			itemID,
			StatusPending,
		); err != nil {
			return nil, fmt.Errorf("reorder paused file transfer batch item: %w", err)
		}
	}

	if err := recalculateBatch(ctx, tx, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit file transfer batch queue update: %w", err)
	}
	for _, item := range removed {
		if err := history.NewStore(s.db).DeleteSourceRef(ctx, history.SourceFileTransfer, item.ID); err != nil {
			return nil, err
		}
	}
	if err := s.syncBatchTransferHistory(ctx, id); err != nil {
		return nil, err
	}
	return removed, nil
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
		WHERE batch_id = ? AND status IN (?, ?, ?, ?)`,
		StatusCanceled,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPendingApproval,
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
	if rows > 0 {
		if err := s.syncBatchTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) FailActive(ctx context.Context, transferError string, batchError string) error {
	now := nowString()
	ids, err := s.transferIDsByStatuses(ctx, StatusPendingApproval, StatusPending, StatusRunning, StatusPaused)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE status IN (?, ?, ?, ?)`,
		StatusFailed,
		strings.TrimSpace(transferError),
		now,
		now,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	); err != nil {
		return fmt.Errorf("fail active file transfers: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE status IN (?, ?, ?, ?)`,
		StatusFailed,
		strings.TrimSpace(batchError),
		now,
		now,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	); err != nil {
		return fmt.Errorf("fail active file transfer batches: %w", err)
	}
	return s.syncTransferHistoryIDs(ctx, ids)
}

func (s *Store) syncTransferHistoryIDs(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) transferIDsByStatuses(ctx context.Context, statuses ...string) ([]int64, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(statuses))
	args := make([]any, 0, len(statuses))
	for _, status := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM file_transfers
		WHERE status IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("read transfer ids for history sync: %w", err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan transfer id for history sync: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transfer ids for history sync: %w", err)
	}
	return ids, nil
}

func (s *Store) RecalculateBatch(ctx context.Context, id int64) error {
	return recalculateBatch(ctx, s.db, id)
}

type batchRecalculator interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func recalculateBatch(ctx context.Context, execer batchRecalculator, id int64) error {
	now := nowString()
	_, err := execer.ExecContext(ctx, `
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
				WHEN completed_items > 0 THEN ?
				WHEN canceled_items > 0 THEN ?
				ELSE ?
			END,
			completed_at = COALESCE(completed_at, ?),
			bytes_per_second = 0,
			eta_seconds = 0,
			updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		StatusFailed,
		StatusCompleted,
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
	if len(filter.TargetIDs) > 0 {
		clauses = append(clauses, "ft.server_id IN ("+placeholders(len(filter.TargetIDs))+")")
		for _, id := range filter.TargetIDs {
			args = append(args, id)
		}
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		clauses = append(clauses, "(ft.remote_path LIKE ? OR ft.local_path LIKE ? OR ft.file_name LIKE ? OR COALESCE(ct.name, '') LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func batchListWhere(filter BatchListFilter) (string, []any) {
	clauses := []string{}
	args := []any{}
	if filter.Direction != "" {
		clauses = append(clauses, "b.direction = ?")
		args = append(args, filter.Direction)
	}
	if filter.Status != "" {
		clauses = append(clauses, "b.status = ?")
		args = append(args, filter.Status)
	}
	if filter.ServerID > 0 {
		clauses = append(clauses, "b.server_id = ?")
		args = append(args, filter.ServerID)
	}
	if len(filter.TargetIDs) > 0 {
		clauses = append(clauses, "b.server_id IN ("+placeholders(len(filter.TargetIDs))+")")
		for _, id := range filter.TargetIDs {
			args = append(args, id)
		}
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		clauses = append(clauses, "(b.archive_name LIKE ? OR COALESCE(ct.name, '') LIKE ?)")
		args = append(args, like, like)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func placeholders(count int) string {
	if count < 1 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = "?"
	}
	return strings.Join(items, ",")
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
	request.Status = strings.TrimSpace(request.Status)
	request.ApprovalNote = strings.TrimSpace(request.ApprovalNote)
	request.ArchiveName = strings.TrimSpace(request.ArchiveName)
	if request.Source == "" {
		request.Source = SourceUI
	}
	if request.Status == "" {
		request.Status = StatusPending
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
	if request.Status != StatusPending && request.Status != StatusPendingApproval {
		return request, fmt.Errorf("status must be pending or pending_approval")
	}
	if err := validatePathLike("approval_note", request.ApprovalNote, false); err != nil {
		return request, err
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
	filter.TargetIDs = normalizeTargetIDs(filter.TargetIDs)
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
	case StatusPending, StatusPendingApproval, StatusRunning, StatusPaused, StatusCompleted, StatusFailed, StatusCanceled:
	default:
		filter.Status = ""
	}
	return filter
}

func normalizeBatchListFilter(filter BatchListFilter) BatchListFilter {
	filter.Direction = strings.TrimSpace(filter.Direction)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.TargetIDs = normalizeTargetIDs(filter.TargetIDs)
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
	case StatusPending, StatusPendingApproval, StatusRunning, StatusPaused, StatusCompleted, StatusFailed, StatusCanceled:
	default:
		filter.Status = ""
	}
	return filter
}

func normalizeTargetIDs(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := map[int64]bool{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value < 1 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
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

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeApprovedItemIDs(values []int64) ([]int64, error) {
	seen := map[int64]bool{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value < 1 || seen[value] {
			return nil, ErrInvalidArgument
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func rejectionNote(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return "rejected by local user"
	}
	return note
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}
