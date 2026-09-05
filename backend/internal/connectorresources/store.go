package connectorresources

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type Store struct {
	db          *sql.DB
	vault       *vault.Vault
	workspaceID string
}

func NewStore(db *sql.DB, secretVault *vault.Vault, workspaceID string) *Store {
	return &Store{db: db, vault: secretVault, workspaceID: strings.TrimSpace(workspaceID)}
}

func (s *Store) Scope(connectorKind, resourceKind string) connectorapi.CredentialResourceStore {
	return &scopedStore{store: s, connectorKind: strings.TrimSpace(connectorKind), resourceKind: strings.TrimSpace(resourceKind)}
}

type scopedStore struct {
	store         *Store
	connectorKind string
	resourceKind  string
}

func (s *scopedStore) validate() error {
	if s == nil || s.store == nil || s.store.db == nil || s.store.vault == nil || s.store.workspaceID == "" {
		return errors.New("connector credential resource store is unavailable")
	}
	if s.connectorKind == "" || s.resourceKind == "" {
		return errors.New("connector credential resource scope is invalid")
	}
	return nil
}

func (s *scopedStore) List(ctx context.Context) ([]connectorapi.CredentialResource, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id, name, resource_type, public_data, fingerprint, created_at, updated_at FROM connector_credential_resources WHERE connector_kind = ? AND resource_kind = ? ORDER BY name`, s.connectorKind, s.resourceKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []connectorapi.CredentialResource{}
	for rows.Next() {
		var item connectorapi.CredentialResource
		if err := rows.Scan(&item.ID, &item.Name, &item.ResourceType, &item.PublicData, &item.Fingerprint, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *scopedStore) Get(ctx context.Context, id int64) (connectorapi.CredentialResource, error) {
	if err := s.validate(); err != nil {
		return connectorapi.CredentialResource{}, err
	}
	var item connectorapi.CredentialResource
	err := s.store.db.QueryRowContext(ctx, `SELECT id, name, resource_type, public_data, fingerprint, created_at, updated_at FROM connector_credential_resources WHERE id = ? AND connector_kind = ? AND resource_kind = ?`, id, s.connectorKind, s.resourceKind).
		Scan(&item.ID, &item.Name, &item.ResourceType, &item.PublicData, &item.Fingerprint, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return connectorapi.CredentialResource{}, connectorapi.ErrCredentialResourceNotFound
	}
	return item, err
}

func (s *scopedStore) GetSecret(ctx context.Context, id int64, destination any) error {
	if err := s.validate(); err != nil {
		return err
	}
	if destination == nil {
		return errors.New("credential resource secret destination is required")
	}
	var encrypted string
	err := s.store.db.QueryRowContext(ctx, `SELECT encrypted_secret FROM connector_credential_resources WHERE id = ? AND connector_kind = ? AND resource_kind = ?`, id, s.connectorKind, s.resourceKind).Scan(&encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return connectorapi.ErrCredentialResourceNotFound
	}
	if err != nil {
		return err
	}
	return recordcrypto.DecryptJSON(s.store.vault, s.store.workspaceID, recordcrypto.ConnectorCredentialResource, id, encrypted, destination)
}

func (s *scopedStore) Create(ctx context.Context, input connectorapi.CreateCredentialResourceInput) (connectorapi.CredentialResource, error) {
	if err := s.validate(); err != nil {
		return connectorapi.CredentialResource{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return connectorapi.CredentialResource{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO connector_credential_resources (connector_kind, resource_kind, name, resource_type, public_data, encrypted_secret, fingerprint, created_at, updated_at) VALUES (?, ?, ?, ?, ?, '', ?, ?, ?)`, s.connectorKind, s.resourceKind, input.Name, input.ResourceType, input.PublicData, input.Fingerprint, now, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			return connectorapi.CredentialResource{}, connectorapi.ErrCredentialResourceNameExists
		}
		return connectorapi.CredentialResource{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return connectorapi.CredentialResource{}, err
	}
	encrypted, err := recordcrypto.EncryptJSON(s.store.vault, s.store.workspaceID, recordcrypto.ConnectorCredentialResource, id, input.Secret)
	if err != nil {
		return connectorapi.CredentialResource{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE connector_credential_resources SET encrypted_secret = ? WHERE id = ?`, encrypted, id); err != nil {
		return connectorapi.CredentialResource{}, err
	}
	if err := tx.Commit(); err != nil {
		return connectorapi.CredentialResource{}, err
	}
	return s.Get(ctx, id)
}

func (s *scopedStore) Update(ctx context.Context, id int64, input connectorapi.UpdateCredentialResourceInput) (connectorapi.CredentialResource, error) {
	if err := s.validate(); err != nil {
		return connectorapi.CredentialResource{}, err
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE connector_credential_resources SET name = ?, public_data = ?, updated_at = ? WHERE id = ? AND connector_kind = ? AND resource_kind = ?`, input.Name, input.PublicData, time.Now().UTC().Format(time.RFC3339), id, s.connectorKind, s.resourceKind)
	if err != nil {
		if isUniqueConstraintError(err) {
			return connectorapi.CredentialResource{}, connectorapi.ErrCredentialResourceNameExists
		}
		return connectorapi.CredentialResource{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return connectorapi.CredentialResource{}, err
	}
	if affected == 0 {
		return connectorapi.CredentialResource{}, connectorapi.ErrCredentialResourceNotFound
	}
	return s.Get(ctx, id)
}

func (s *scopedStore) Delete(ctx context.Context, id int64) error {
	if err := s.validate(); err != nil {
		return err
	}
	result, err := s.store.db.ExecContext(ctx, `DELETE FROM connector_credential_resources WHERE id = ? AND connector_kind = ? AND resource_kind = ?`, id, s.connectorKind, s.resourceKind)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return connectorapi.ErrCredentialResourceNotFound
	}
	return nil
}

func (s *scopedStore) CountProfileReferences(ctx context.Context, publicField string, numericValue int64) (int, error) {
	if err := s.validate(); err != nil {
		return 0, err
	}
	if strings.TrimSpace(publicField) == "" {
		return 0, errors.New("credential profile reference field is required")
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT p.public_json FROM connector_credential_profiles p JOIN connector_targets t ON t.id = p.target_id AND t.connector_kind = p.connector_kind WHERE p.connector_kind = ? AND p.status = 'active' AND t.status = 'active'`, s.connectorKind)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return 0, err
		}
		var metadata map[string]any
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			return 0, err
		}
		if numericMetadataValue(metadata[publicField]) == numericValue {
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func numericMetadataValue(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := strconv.ParseInt(string(typed), 10, 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func isUniqueConstraintError(err error) bool {
	message := strings.ToLower(fmt.Sprint(err))
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "is not unique")
}
