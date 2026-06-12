package sshkeys

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/vault"
	"golang.org/x/crypto/ssh"
)

const (
	TypeED25519 = "ed25519"
	TypeRSA     = "rsa"
	TypeECDSA   = "ecdsa"

	maxImportedPrivateKeyBytes = 64 * 1024
	minImportedRSABits         = 2048
)

type SSHKey struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	KeyType        string `json:"key_type"`
	PublicKey      string `json:"public_key"`
	Fingerprint    string `json:"fingerprint"`
	InstallCommand string `json:"install_command"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type CreateRequest struct {
	Name    string `json:"name"`
	KeyType string `json:"key_type"`
}

type ImportRequest struct {
	Name       string `json:"name"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}

type UpdateRequest struct {
	Name string `json:"name"`
}

type privateKeySecret struct {
	PrivateKey string `json:"private_key"`
}

type PrivateKey struct {
	ID         int64
	Name       string
	KeyType    string
	PrivateKey string
}

type Store struct {
	db    *sql.DB
	vault *vault.Vault
}

func NewStore(db *sql.DB, vault *vault.Vault) *Store {
	return &Store{db: db, vault: vault}
}

func (s *Store) List(ctx context.Context) ([]SSHKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, key_type, public_key, fingerprint, created_at, updated_at FROM ssh_keys ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list ssh keys: %w", err)
	}
	defer rows.Close()

	items := []SSHKey{}
	for rows.Next() {
		var item SSHKey
		if err := rows.Scan(&item.ID, &item.Name, &item.KeyType, &item.PublicKey, &item.Fingerprint, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan ssh key: %w", err)
		}
		item.InstallCommand = InstallCommand(item.PublicKey)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ssh keys: %w", err)
	}
	return items, nil
}

func (s *Store) Get(ctx context.Context, id int64) (SSHKey, error) {
	var item SSHKey
	err := s.db.QueryRowContext(ctx, `SELECT id, name, key_type, public_key, fingerprint, created_at, updated_at FROM ssh_keys WHERE id = ?`, id).
		Scan(&item.ID, &item.Name, &item.KeyType, &item.PublicKey, &item.Fingerprint, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SSHKey{}, ErrNotFound
	}
	if err != nil {
		return SSHKey{}, fmt.Errorf("get ssh key: %w", err)
	}
	item.InstallCommand = InstallCommand(item.PublicKey)
	return item, nil
}

func (s *Store) GetPrivateKey(ctx context.Context, id int64) (PrivateKey, error) {
	var encrypted string
	var item PrivateKey
	err := s.db.QueryRowContext(ctx, `SELECT id, name, key_type, encrypted_private_key FROM ssh_keys WHERE id = ?`, id).
		Scan(&item.ID, &item.Name, &item.KeyType, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return PrivateKey{}, ErrNotFound
	}
	if err != nil {
		return PrivateKey{}, fmt.Errorf("get private ssh key: %w", err)
	}

	var secret privateKeySecret
	if err := s.vault.DecryptJSON(encrypted, &secret); err != nil {
		return PrivateKey{}, err
	}
	item.PrivateKey = secret.PrivateKey
	return item, nil
}

func (s *Store) Create(ctx context.Context, request CreateRequest) (SSHKey, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.KeyType = strings.TrimSpace(request.KeyType)
	if request.Name == "" {
		return SSHKey{}, ValidationError("name is required")
	}
	if err := validateName(request.Name); err != nil {
		return SSHKey{}, err
	}
	if request.KeyType == "" {
		request.KeyType = TypeED25519
	}

	privateKey, publicKey, fingerprint, err := generateKey(request.Name, request.KeyType)
	if err != nil {
		return SSHKey{}, err
	}

	encrypted, err := s.vault.EncryptJSON(privateKeySecret{PrivateKey: privateKey})
	if err != nil {
		return SSHKey{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO ssh_keys (name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		request.Name,
		request.KeyType,
		publicKey,
		encrypted,
		fingerprint,
		now,
		now,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return SSHKey{}, ValidationError("ssh key name already exists")
		}
		return SSHKey{}, fmt.Errorf("create ssh key: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SSHKey{}, fmt.Errorf("read ssh key id: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Import(ctx context.Context, request ImportRequest) (SSHKey, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.PrivateKey = strings.TrimSpace(request.PrivateKey)
	if request.Name == "" {
		return SSHKey{}, ValidationError("name is required")
	}
	if err := validateName(request.Name); err != nil {
		return SSHKey{}, err
	}
	if request.PrivateKey == "" {
		return SSHKey{}, ValidationError("private_key is required")
	}
	if len([]byte(request.PrivateKey)) > maxImportedPrivateKeyBytes {
		return SSHKey{}, ValidationError("private_key is too large")
	}

	privateKey, publicKey, fingerprint, keyType, err := parseImportedKey(request.Name, request.PrivateKey, request.Passphrase)
	if err != nil {
		return SSHKey{}, err
	}

	encrypted, err := s.vault.EncryptJSON(privateKeySecret{PrivateKey: privateKey})
	if err != nil {
		return SSHKey{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO ssh_keys (name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		request.Name,
		keyType,
		publicKey,
		encrypted,
		fingerprint,
		now,
		now,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return SSHKey{}, ValidationError("ssh key name already exists")
		}
		return SSHKey{}, fmt.Errorf("import ssh key: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SSHKey{}, fmt.Errorf("read ssh key id: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Update(ctx context.Context, id int64, request UpdateRequest) (SSHKey, error) {
	request.Name = strings.TrimSpace(request.Name)
	if id < 1 {
		return SSHKey{}, ErrNotFound
	}
	if request.Name == "" {
		return SSHKey{}, ValidationError("name is required")
	}
	if err := validateName(request.Name); err != nil {
		return SSHKey{}, err
	}

	existing, err := s.Get(ctx, id)
	if err != nil {
		return SSHKey{}, err
	}
	publicKey, err := publicKeyWithComment(existing.PublicKey, "aipermission-"+request.Name)
	if err != nil {
		return SSHKey{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `UPDATE ssh_keys SET name = ?, public_key = ?, updated_at = ? WHERE id = ?`, request.Name, publicKey, now, id)
	if err != nil {
		if isUniqueConstraintError(err) {
			return SSHKey{}, ValidationError("ssh key name already exists")
		}
		return SSHKey{}, fmt.Errorf("update ssh key: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return SSHKey{}, fmt.Errorf("read rows affected: %w", err)
	}
	if affected == 0 {
		return SSHKey{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

func validateName(name string) error {
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

func publicKeyWithComment(publicKey string, comment string) (string, error) {
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", fmt.Errorf("parse ssh public key: %w", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(parsed))) + " " + comment, nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	usageCount, err := s.connectorProfileUsageCount(ctx, id)
	if err != nil {
		return fmt.Errorf("check ssh key usage: %w", err)
	}
	if usageCount > 0 {
		return ValidationError("ssh key is used by one or more SSH connector profiles")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM ssh_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete ssh key: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) connectorProfileUsageCount(ctx context.Context, id int64) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT public_json FROM connector_credential_profiles WHERE connector_kind = 'ssh'`)
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
		if int64MetadataValue(metadata, "ssh_key_id") == id {
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func int64MetadataValue(metadata map[string]any, key string) int64 {
	value, ok := metadata[key]
	if !ok || value == nil {
		return 0
	}
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
	default:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
		return parsed
	}
}

func generateKey(name string, keyType string) (string, string, string, error) {
	comment := "aipermission-" + name

	switch keyType {
	case TypeED25519:
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", "", "", fmt.Errorf("generate ed25519 key: %w", err)
		}
		return marshalKey(private, public, comment)
	case TypeRSA:
		private, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return "", "", "", fmt.Errorf("generate rsa key: %w", err)
		}
		return marshalKey(private, &private.PublicKey, comment)
	default:
		return "", "", "", ValidationError("key_type must be ed25519 or rsa")
	}
}

func marshalKey(private any, public any, comment string) (string, string, string, error) {
	privateBlock, err := ssh.MarshalPrivateKey(private, comment)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal private key: %w", err)
	}
	privateKey := string(pem.EncodeToMemory(privateBlock))

	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal public key: %w", err)
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic))) + " " + comment
	fingerprint := ssh.FingerprintSHA256(sshPublic)
	return privateKey, publicKey, fingerprint, nil
}

func parseImportedKey(name string, privateKey string, passphrase string) (string, string, string, string, error) {
	raw, err := parseRawPrivateKey([]byte(privateKey), passphrase)
	if err != nil {
		return "", "", "", "", err
	}
	if private, ok := raw.(*rsa.PrivateKey); ok && private.N.BitLen() < minImportedRSABits {
		return "", "", "", "", ValidationError("private_key rsa keys must be at least 2048 bits")
	}

	signer, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		return "", "", "", "", ValidationError("private_key could not be parsed")
	}
	keyType, err := importedKeyType(signer.PublicKey().Type())
	if err != nil {
		return "", "", "", "", err
	}

	comment := "aipermission-" + name
	privateBlock, err := ssh.MarshalPrivateKey(raw, comment)
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal imported private key: %w", err)
	}
	normalizedPrivateKey := string(pem.EncodeToMemory(privateBlock))
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + comment
	fingerprint := ssh.FingerprintSHA256(signer.PublicKey())
	return normalizedPrivateKey, publicKey, fingerprint, keyType, nil
}

func parseRawPrivateKey(privateKey []byte, passphrase string) (any, error) {
	if strings.TrimSpace(passphrase) != "" {
		raw, err := ssh.ParseRawPrivateKeyWithPassphrase(privateKey, []byte(passphrase))
		if err != nil {
			return nil, ValidationError("private_key could not be parsed with the provided passphrase")
		}
		return raw, nil
	}
	raw, err := ssh.ParseRawPrivateKey(privateKey)
	if err != nil {
		return nil, ValidationError("private_key could not be parsed")
	}
	return raw, nil
}

func importedKeyType(publicType string) (string, error) {
	switch publicType {
	case ssh.KeyAlgoED25519:
		return TypeED25519, nil
	case ssh.KeyAlgoRSA:
		return TypeRSA, nil
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return TypeECDSA, nil
	default:
		return "", ValidationError("private_key type is not supported")
	}
}

func InstallCommand(publicKey string) string {
	return fmt.Sprintf(`mkdir -p ~/.ssh && chmod 700 ~/.ssh && printf '%%s\n' %s >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys`, shellQuote(publicKey))
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
