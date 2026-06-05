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
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
)

const maxPathRunes = 4096

var ErrNotFound = errors.New("file transfer not found")

type Record struct {
	ID               int64  `json:"id"`
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
	ChecksumSHA256   string `json:"checksum_sha256"`
	Error            string `json:"error"`
	CreatedAt        string `json:"created_at"`
	StartedAt        string `json:"started_at,omitempty"`
	CompletedAt      string `json:"completed_at,omitempty"`
	UpdatedAt        string `json:"updated_at"`

	TempPath string `json:"-"`
}

type CreateRequest struct {
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
			server_id, direction, source, status, local_path, remote_path, file_name,
			size_bytes, transferred_bytes, temp_path, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		SELECT ft.id, ft.server_id, COALESCE(srv.name, ''), ft.direction, ft.source, ft.status,
			ft.local_path, ft.remote_path, ft.file_name, ft.size_bytes, ft.transferred_bytes,
			ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
			COALESCE(ft.completed_at, ''), ft.updated_at
		FROM file_transfers ft
		LEFT JOIN servers srv ON srv.id = ft.server_id
		WHERE ft.id = ?`,
		id,
	).Scan(
		&item.ID,
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
		SELECT ft.id, ft.server_id, COALESCE(srv.name, ''), ft.direction, ft.source, ft.status,
			ft.local_path, ft.remote_path, ft.file_name, ft.size_bytes, ft.transferred_bytes,
			ft.checksum_sha256, ft.temp_path, ft.error, ft.created_at, COALESCE(ft.started_at, ''),
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
		if err := rows.Scan(
			&item.ID,
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
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate file transfers: %w", err)
	}
	return items, total, nil
}

func (s *Store) MarkRunning(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE id = ? AND status = ?`,
		StatusRunning,
		now,
		now,
		id,
		StatusPending,
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

func (s *Store) UpdateProgress(ctx context.Context, id int64, transferred int64, size int64) error {
	if transferred < 0 {
		transferred = 0
	}
	if size < 0 {
		size = 0
	}
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET transferred_bytes = ?, size_bytes = CASE WHEN ? > 0 THEN ? ELSE size_bytes END, updated_at = ?
		WHERE id = ? AND status = ?`,
		transferred,
		size,
		size,
		now,
		id,
		StatusRunning,
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
		WHERE id = ? AND status = ?`,
		StatusCompleted,
		transferred,
		transferred,
		strings.TrimSpace(checksum),
		now,
		now,
		id,
		StatusRunning,
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
		WHERE id = ? AND status IN (?, ?)`,
		StatusFailed,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPending,
		StatusRunning,
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
		WHERE id = ? AND status IN (?, ?)`,
		StatusCanceled,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPending,
		StatusRunning,
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
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed, StatusCanceled:
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

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}
