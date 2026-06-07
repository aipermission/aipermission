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
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/vault"
)

const (
	RuleAlwaysRun        = "always_run"
	RuleApprovalRequired = "approval_required"
	RuleBlocked          = "blocked"
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

type Permission struct {
	ServerID      int64  `json:"server_id"`
	ServerName    string `json:"server_name"`
	ExecutionRule string `json:"execution_rule"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type PermissionInput struct {
	ServerID      int64  `json:"server_id"`
	ExecutionRule string `json:"execution_rule"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type UpdatePermissionsRequest struct {
	Permissions []PermissionInput `json:"permissions"`
}

type Store struct {
	db    *sql.DB
	vault *vault.Vault
}

func NewStore(db *sql.DB, secretVault ...*vault.Vault) *Store {
	store := &Store{db: db}
	if len(secretVault) > 0 {
		store.vault = secretVault[0]
	}
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
		item.TokenValue = s.decryptTokenValue(item.TokenValue)
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
	item.TokenValue = s.decryptTokenValue(item.TokenValue)
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
	if s.vault != nil && storedTokenValue != "" {
		storedTokenValue, err = s.vault.EncryptJSON(tokenValueSecret{Token: tokenValue})
		if err != nil {
			return CreateResponse{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (name, token_hash, token_prefix, token_value, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, request.Name, tokenHash, tokenPrefix, storedTokenValue, expiresAt, now, now)
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

func (s *Store) ListPermissions(ctx context.Context, tokenID int64) ([]Permission, error) {
	if _, err := s.Get(ctx, tokenID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT p.server_id, srv.name, p.execution_rule, COALESCE(p.expires_at, ''), p.created_at, p.updated_at FROM token_server_permissions p JOIN servers srv ON srv.id = p.server_id WHERE p.token_id = ? ORDER BY srv.name`, tokenID)
	if err != nil {
		return nil, fmt.Errorf("list token permissions: %w", err)
	}
	defer rows.Close()

	items := []Permission{}
	for rows.Next() {
		var item Permission
		if err := rows.Scan(&item.ServerID, &item.ServerName, &item.ExecutionRule, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan token permission: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate token permissions: %w", err)
	}
	return items, nil
}

func (s *Store) UpdatePermissions(ctx context.Context, tokenID int64, request UpdatePermissionsRequest) ([]Permission, error) {
	if _, err := s.Get(ctx, tokenID); err != nil {
		return nil, err
	}
	if err := validatePermissions(ctx, s.db, request.Permissions); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin permission update: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM token_server_permissions WHERE token_id = ?`, tokenID); err != nil {
		return nil, fmt.Errorf("clear token permissions: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, permission := range request.Permissions {
		expiresAt, err := normalizePermissionExpiresAt(permission)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO token_server_permissions (token_id, server_id, execution_rule, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, tokenID, permission.ServerID, permission.ExecutionRule, expiresAt, now, now); err != nil {
			return nil, fmt.Errorf("insert token permission: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit permission update: %w", err)
	}
	return s.ListPermissions(ctx, tokenID)
}

func validatePermissions(ctx context.Context, db *sql.DB, permissions []PermissionInput) error {
	seen := map[int64]bool{}
	for _, permission := range permissions {
		if permission.ServerID < 1 {
			return ValidationError("server_id is required")
		}
		if seen[permission.ServerID] {
			return ValidationError("server_id must be unique per token")
		}
		seen[permission.ServerID] = true
		if !validRule(permission.ExecutionRule) {
			return ValidationError("execution_rule must be always_run, approval_required, or blocked")
		}
		if _, err := normalizePermissionExpiresAt(permission); err != nil {
			return err
		}
		var exists int
		err := db.QueryRowContext(ctx, `SELECT 1 FROM servers WHERE id = ?`, permission.ServerID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ValidationError("server_id does not exist")
		}
		if err != nil {
			return fmt.Errorf("validate server: %w", err)
		}
	}
	return nil
}

func validRule(rule string) bool {
	switch rule {
	case RuleAlwaysRun, RuleApprovalRequired, RuleBlocked:
		return true
	default:
		return false
	}
}

func normalizeTokenExpiresAt(value string) (string, error) {
	return normalizeFutureTimestamp("expires_at", value)
}

func normalizePermissionExpiresAt(permission PermissionInput) (string, error) {
	value := strings.TrimSpace(permission.ExpiresAt)
	if value == "" {
		return "", nil
	}
	if permission.ExecutionRule == RuleBlocked {
		return "", ValidationError("expires_at is not supported for blocked permissions")
	}
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

func (s *Store) decryptTokenValue(value string) string {
	if s.vault == nil || value == "" {
		return value
	}
	var secret tokenValueSecret
	if err := s.vault.DecryptJSON(value, &secret); err == nil {
		return secret.Token
	} else {
		log.Printf("reusable token value could not be decrypted; returning empty token value: %v", err)
	}
	return ""
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
