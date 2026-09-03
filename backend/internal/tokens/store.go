package tokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type Token struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	TokenPrefix string `json:"-"`
	TokenValue  string `json:"token,omitempty"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type CreateRequest struct {
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type CreateOptions struct {
	StoreReusableToken bool
}

type CreateResponse struct {
	Token
}

type Store struct {
	db          storeDB
	begin       func(context.Context, *sql.TxOptions) (*sql.Tx, error)
	vault       *vault.Vault
	workspaceID string
}

type storeDB = sqldb.Executor

func NewStore(db *sql.DB, secretVault ...*vault.Vault) *Store {
	store := &Store{db: db, begin: db.BeginTx}
	if len(secretVault) > 0 {
		store.vault = secretVault[0]
	}
	return store
}

func NewEncryptedStore(db *sql.DB, secretVault *vault.Vault, workspaceID string) *Store {
	return &Store{db: db, begin: db.BeginTx, vault: secretVault, workspaceID: workspaceID}
}

func NewTxStore(tx *sql.Tx, secretVault ...*vault.Vault) *Store {
	store := &Store{db: tx}
	if len(secretVault) > 0 {
		store.vault = secretVault[0]
	}
	return store
}

func (s *Store) WithTx(tx *sql.Tx) *Store {
	if s == nil {
		return NewTxStore(tx)
	}
	store := NewTxStore(tx, s.vault)
	store.workspaceID = s.workspaceID
	return store
}

func (s *Store) List(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, token_prefix, token_value, COALESCE(revoked_at, ''), COALESCE(expires_at, ''), created_at, updated_at FROM api_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	items := []Token{}
	for rows.Next() {
		var item Token
		if err := rows.Scan(&item.ID, &item.Name, &item.TokenPrefix, &item.TokenValue, &item.RevokedAt, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		item.TokenValue, err = s.decryptTokenValue(item.ID, item.TokenValue)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tokens: %w", err)
	}
	return items, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Token, error) {
	var item Token
	err := s.db.QueryRowContext(ctx, `SELECT id, name, token_prefix, token_value, COALESCE(revoked_at, ''), COALESCE(expires_at, ''), created_at, updated_at FROM api_tokens WHERE id = ?`, id).
		Scan(&item.ID, &item.Name, &item.TokenPrefix, &item.TokenValue, &item.RevokedAt, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	if err != nil {
		return Token{}, fmt.Errorf("get token: %w", err)
	}
	item.TokenValue, err = s.decryptTokenValue(item.ID, item.TokenValue)
	if err != nil {
		return Token{}, err
	}
	return item, nil
}

func (s *Store) Create(ctx context.Context, request CreateRequest, options ...CreateOptions) (CreateResponse, error) {
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return CreateResponse{}, ValidationError("name is required")
	}
	if err := validateTokenName(request.Name); err != nil {
		return CreateResponse{}, err
	}
	expiresAt, err := normalizeTokenExpiresAt(request.ExpiresAt)
	if err != nil {
		return CreateResponse{}, err
	}
	createOptions := CreateOptions{}
	if len(options) > 0 {
		createOptions = options[0]
	}

	tokenValue, err := generateToken()
	if err != nil {
		return CreateResponse{}, err
	}
	tokenHash := HashToken(tokenValue)
	tokenPrefix := tokenValue[:min(16, len(tokenValue))]
	storedTokenValue := ""
	if createOptions.StoreReusableToken {
		storedTokenValue = tokenValue
	}
	now := time.Now().UTC().Format(time.RFC3339)

	if s.begin != nil {
		tx, err := s.begin(ctx, nil)
		if err != nil {
			return CreateResponse{}, fmt.Errorf("begin create token: %w", err)
		}
		defer tx.Rollback()
		item, err := s.WithTx(tx).create(ctx, request, tokenValue, tokenHash, tokenPrefix, storedTokenValue, expiresAt, now)
		if err != nil {
			return CreateResponse{}, err
		}
		if err := tx.Commit(); err != nil {
			return CreateResponse{}, fmt.Errorf("commit create token: %w", err)
		}
		return item, nil
	}
	return s.create(ctx, request, tokenValue, tokenHash, tokenPrefix, storedTokenValue, expiresAt, now)
}

func (s *Store) create(ctx context.Context, request CreateRequest, tokenValue, tokenHash, tokenPrefix, storedTokenValue, expiresAt, now string) (CreateResponse, error) {
	insertedTokenValue := storedTokenValue
	if s.vault != nil && storedTokenValue != "" {
		if strings.TrimSpace(s.workspaceID) == "" {
			return CreateResponse{}, fmt.Errorf("workspace ID is required for reusable token encryption")
		}
		insertedTokenValue = ""
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (name, token_hash, token_prefix, token_value, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, request.Name, tokenHash, tokenPrefix, insertedTokenValue, expiresAt, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return CreateResponse{}, ValidationError("token name already exists")
		}
		return CreateResponse{}, fmt.Errorf("create token: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return CreateResponse{}, fmt.Errorf("read token id: %w", err)
	}
	if s.vault != nil && storedTokenValue != "" {
		encrypted, err := recordcrypto.EncryptJSON(s.vault, s.workspaceID, recordcrypto.APIToken, id, tokenValueSecret{Token: tokenValue})
		if err != nil {
			return CreateResponse{}, fmt.Errorf("encrypt reusable token: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET token_value = ? WHERE id = ?`, encrypted, id); err != nil {
			return CreateResponse{}, fmt.Errorf("store encrypted reusable token: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
		SELECT ?, id, 1, ?, ? FROM projects WHERE status = 'active'`, id, now, now); err != nil {
		return CreateResponse{}, fmt.Errorf("initialize token project scopes: %w", err)
	}
	item, err := s.Get(ctx, id)
	if err != nil {
		return CreateResponse{}, err
	}
	item.TokenValue = tokenValue
	return CreateResponse{Token: item}, nil
}

func (s *Store) Revoke(ctx context.Context, id int64) (Token, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, ?), updated_at = ? WHERE id = ?`, now, now, id)
	if err != nil {
		return Token{}, fmt.Errorf("revoke token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Token{}, fmt.Errorf("read rows affected: %w", err)
	}
	if affected == 0 {
		return Token{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

func normalizeTokenExpiresAt(value string) (string, error) {
	return normalizeFutureTimestamp("expires_at", value)
}

func normalizeFutureTimestamp(field string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", ValidationError(field + " must be an RFC3339 timestamp")
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(time.Now().UTC()) {
		return "", ValidationError(field + " must be in the future")
	}
	return expiresAt.Format(time.RFC3339), nil
}

func generateToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return "aip_" + base64.RawURLEncoding.EncodeToString(value), nil
}

type tokenValueSecret struct {
	Token string `json:"token"`
}

func HashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Store) decryptTokenValue(id int64, value string) (string, error) {
	if value == "" {
		return value, nil
	}
	if s.vault == nil {
		if vault.IsRecordEnvelope(value) {
			return "", fmt.Errorf("decrypt reusable token %d: encrypted store is not configured", id)
		}
		return value, nil
	}
	var secret tokenValueSecret
	if strings.TrimSpace(s.workspaceID) == "" {
		return "", fmt.Errorf("decrypt reusable token %d: workspace ID is missing", id)
	}
	if err := recordcrypto.DecryptJSON(s.vault, s.workspaceID, recordcrypto.APIToken, id, value, &secret); err != nil {
		return "", fmt.Errorf("decrypt reusable token %d: %w", id, err)
	}
	return secret.Token, nil
}

func validateTokenName(name string) error {
	if len([]rune(name)) > 80 {
		return ValidationError("name must be 80 characters or fewer")
	}
	for _, r := range name {
		if !unicode.IsPrint(r) || r == '\r' || r == '\n' {
			return ValidationError("name must be printable and single-line")
		}
	}
	return nil
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
