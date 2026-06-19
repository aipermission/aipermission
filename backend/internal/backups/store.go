package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("backup provider not found")
var ErrRecordNotFound = errors.New("backup record not found")

type ValidationError string

func (e ValidationError) Error() string {
	return string(e)
}

type Provider struct {
	ID                  int64
	ProviderType        string
	Name                string
	Status              string
	Public              map[string]any
	EncryptedSecretJSON string
	LastCheckedAt       *string
	CreatedAt           string
	UpdatedAt           string
}

type Record struct {
	ID              int64
	ProviderID      int64
	DatabaseID      string
	DatabaseName    string
	ProviderFileID  string
	Filename        string
	SourceMachine   string
	SizeBytes       int64
	ChecksumSHA256  string
	BackupCreatedAt string
	UploadedAt      string
	Metadata        map[string]any
	DeletedAt       *string
	CreatedAt       string
	UpdatedAt       string
}

type CreateProviderRequest struct {
	ProviderType string
	Name         string
	Public       map[string]any
	Encrypted    string
}

type UpdateProviderRequest struct {
	Name      string
	Status    string
	Public    map[string]any
	Encrypted *string
}

type ListRecordsFilter struct {
	ProviderID     int64
	DatabaseName   string
	IncludeDeleted bool
}

type CreateRecordRequest struct {
	ProviderID      int64
	DatabaseID      string
	DatabaseName    string
	ProviderFileID  string
	Filename        string
	SourceMachine   string
	SizeBytes       int64
	ChecksumSHA256  string
	BackupCreatedAt string
	UploadedAt      string
	Metadata        map[string]any
}

type Store struct {
	db *sql.DB
}

func NewStore(database *sql.DB) *Store {
	return &Store{db: database}
}

func (s *Store) ListProviders(ctx context.Context) ([]Provider, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider_type, name, status, public_json, encrypted_secret_json,
			last_checked_at, created_at, updated_at
		FROM backup_providers
		WHERE status != 'archived'
		ORDER BY provider_type, name`)
	if err != nil {
		return nil, fmt.Errorf("list backup providers: %w", err)
	}
	defer rows.Close()

	var items []Provider
	for rows.Next() {
		item, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list backup providers: %w", err)
	}
	return items, nil
}

func (s *Store) CreateProvider(ctx context.Context, request CreateProviderRequest) (Provider, error) {
	providerType := normalizeProviderType(request.ProviderType)
	if !SupportedProviderType(providerType) {
		return Provider{}, ValidationError("unsupported backup provider type")
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return Provider{}, ValidationError("backup provider name is required")
	}
	publicJSON, err := marshalJSONObject(request.Public)
	if err != nil {
		return Provider{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO backup_providers (
			provider_type, name, status, public_json, encrypted_secret_json, created_at, updated_at
		)
		VALUES (?, ?, 'active', ?, ?, ?, ?)`,
		providerType,
		name,
		publicJSON,
		request.Encrypted,
		now,
		now,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return Provider{}, ValidationError("backup provider name already exists")
		}
		return Provider{}, fmt.Errorf("create backup provider: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Provider{}, fmt.Errorf("read backup provider id: %w", err)
	}
	return s.GetProvider(ctx, id)
}

func (s *Store) GetProvider(ctx context.Context, id int64) (Provider, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, provider_type, name, status, public_json, encrypted_secret_json,
			last_checked_at, created_at, updated_at
		FROM backup_providers
		WHERE id = ? AND status != 'archived'`, id)
	item, err := scanProvider(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Provider{}, ErrNotFound
		}
		return Provider{}, err
	}
	return item, nil
}

func (s *Store) UpdateProvider(ctx context.Context, id int64, request UpdateProviderRequest) (Provider, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return Provider{}, ValidationError("backup provider name is required")
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "disabled" {
		return Provider{}, ValidationError("backup provider status must be active or disabled")
	}
	publicJSON, err := marshalJSONObject(request.Public)
	if err != nil {
		return Provider{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if request.Encrypted != nil {
		result, err := s.db.ExecContext(ctx, `
			UPDATE backup_providers
			SET name = ?, status = ?, public_json = ?, encrypted_secret_json = ?, updated_at = ?
			WHERE id = ? AND status != 'archived'`,
			name, status, publicJSON, *request.Encrypted, now, id,
		)
		if err != nil {
			if isUniqueConstraintError(err) {
				return Provider{}, ValidationError("backup provider name already exists")
			}
			return Provider{}, fmt.Errorf("update backup provider: %w", err)
		}
		if err := requireAffectedProviderRow(result); err != nil {
			return Provider{}, err
		}
	} else {
		result, err := s.db.ExecContext(ctx, `
			UPDATE backup_providers
			SET name = ?, status = ?, public_json = ?, updated_at = ?
			WHERE id = ? AND status != 'archived'`,
			name, status, publicJSON, now, id,
		)
		if err != nil {
			if isUniqueConstraintError(err) {
				return Provider{}, ValidationError("backup provider name already exists")
			}
			return Provider{}, fmt.Errorf("update backup provider: %w", err)
		}
		if err := requireAffectedProviderRow(result); err != nil {
			return Provider{}, err
		}
	}
	return s.GetProvider(ctx, id)
}

func (s *Store) ArchiveProvider(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE backup_providers
		SET status = 'archived', updated_at = ?
		WHERE id = ? AND status != 'archived'`, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("archive backup provider: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("archive backup provider: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func requireAffectedProviderRow(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read backup provider rows affected: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListRecords(ctx context.Context, filter ListRecordsFilter) ([]Record, error) {
	if filter.ProviderID < 1 {
		return nil, ValidationError("provider_id is required")
	}
	query := `
		SELECT id, provider_id, database_id, database_name, provider_file_id, filename,
			source_machine, size_bytes, checksum_sha256, backup_created_at, uploaded_at,
			metadata_json, deleted_at, created_at, updated_at
		FROM backup_records
		WHERE provider_id = ?`
	args := []any{filter.ProviderID}
	if strings.TrimSpace(filter.DatabaseName) != "" {
		query += ` AND database_name = ?`
		args = append(args, strings.TrimSpace(filter.DatabaseName))
	}
	if !filter.IncludeDeleted {
		query += ` AND deleted_at IS NULL`
	}
	query += ` ORDER BY backup_created_at DESC, id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list backup records: %w", err)
	}
	defer rows.Close()

	var items []Record
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list backup records: %w", err)
	}
	return items, nil
}

func (s *Store) CreateRecord(ctx context.Context, request CreateRecordRequest) (Record, error) {
	if request.ProviderID < 1 {
		return Record{}, ValidationError("provider_id is required")
	}
	databaseID := strings.TrimSpace(request.DatabaseID)
	if databaseID == "" {
		return Record{}, ValidationError("database_id is required")
	}
	databaseName := strings.TrimSpace(request.DatabaseName)
	if databaseName == "" {
		return Record{}, ValidationError("database_name is required")
	}
	providerFileID := strings.TrimSpace(request.ProviderFileID)
	if providerFileID == "" {
		return Record{}, ValidationError("provider_file_id is required")
	}
	filename := strings.TrimSpace(request.Filename)
	if filename == "" {
		return Record{}, ValidationError("filename is required")
	}
	backupCreatedAt := strings.TrimSpace(request.BackupCreatedAt)
	if backupCreatedAt == "" {
		backupCreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	uploadedAt := strings.TrimSpace(request.UploadedAt)
	if uploadedAt == "" {
		uploadedAt = time.Now().UTC().Format(time.RFC3339)
	}
	metadataJSON, err := marshalJSONObject(request.Metadata)
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO backup_records (
			provider_id, database_id, database_name, provider_file_id, filename,
			source_machine, size_bytes, checksum_sha256, backup_created_at, uploaded_at,
			metadata_json, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		request.ProviderID,
		databaseID,
		databaseName,
		providerFileID,
		filename,
		strings.TrimSpace(request.SourceMachine),
		request.SizeBytes,
		strings.TrimSpace(request.ChecksumSHA256),
		backupCreatedAt,
		uploadedAt,
		metadataJSON,
		now,
		now,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return Record{}, ValidationError("backup record already exists for this provider file")
		}
		return Record{}, fmt.Errorf("create backup record: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Record{}, fmt.Errorf("read backup record id: %w", err)
	}
	return s.GetRecord(ctx, request.ProviderID, id)
}

func (s *Store) GetRecord(ctx context.Context, providerID int64, id int64) (Record, error) {
	if providerID < 1 {
		return Record{}, ValidationError("provider_id is required")
	}
	if id < 1 {
		return Record{}, ValidationError("backup record id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, provider_id, database_id, database_name, provider_file_id, filename,
			source_machine, size_bytes, checksum_sha256, backup_created_at, uploaded_at,
			metadata_json, deleted_at, created_at, updated_at
		FROM backup_records
		WHERE id = ? AND provider_id = ? AND deleted_at IS NULL`, id, providerID)
	item, err := scanRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, ErrRecordNotFound
		}
		return Record{}, err
	}
	return item, nil
}

func SupportedProviderType(providerType string) bool {
	switch normalizeProviderType(providerType) {
	case "google_drive":
		return true
	default:
		return false
	}
}

func normalizeProviderType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func marshalJSONObject(value map[string]any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal json object: %w", err)
	}
	if !json.Valid(encoded) {
		return "", ValidationError("invalid json object")
	}
	return string(encoded), nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "constraint failed")
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProvider(row rowScanner) (Provider, error) {
	var item Provider
	var publicJSON string
	var lastChecked sql.NullString
	if err := row.Scan(
		&item.ID,
		&item.ProviderType,
		&item.Name,
		&item.Status,
		&publicJSON,
		&item.EncryptedSecretJSON,
		&lastChecked,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return Provider{}, err
	}
	item.Public = map[string]any{}
	if strings.TrimSpace(publicJSON) != "" {
		if err := json.Unmarshal([]byte(publicJSON), &item.Public); err != nil {
			return Provider{}, fmt.Errorf("decode backup provider public json: %w", err)
		}
	}
	if lastChecked.Valid {
		item.LastCheckedAt = &lastChecked.String
	}
	return item, nil
}

func scanRecord(row rowScanner) (Record, error) {
	var item Record
	var metadataJSON string
	var deletedAt sql.NullString
	if err := row.Scan(
		&item.ID,
		&item.ProviderID,
		&item.DatabaseID,
		&item.DatabaseName,
		&item.ProviderFileID,
		&item.Filename,
		&item.SourceMachine,
		&item.SizeBytes,
		&item.ChecksumSHA256,
		&item.BackupCreatedAt,
		&item.UploadedAt,
		&metadataJSON,
		&deletedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return Record{}, err
	}
	item.Metadata = map[string]any{}
	if strings.TrimSpace(metadataJSON) != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &item.Metadata); err != nil {
			return Record{}, fmt.Errorf("decode backup record metadata json: %w", err)
		}
	}
	if deletedAt.Valid {
		item.DeletedAt = &deletedAt.String
	}
	return item, nil
}
