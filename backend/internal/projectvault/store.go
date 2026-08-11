package projectvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/vault"
)

const (
	maxValueBytes         = 16 * 1024
	maxItemsPerDatabase   = 5000
	maxItemsPerOwner      = 1000
	maxProjectsPerItem    = 16
	maxTagsPerItem        = 32
	maxUsageNotesPerItem  = 64
	maxNameRunes          = 128
	maxMetadataRunes      = 1000
	maxMetadataListPage   = 100
	defaultExpiryWarnDays = 14
	workspaceUUIDSetting  = "workspace_uuid"
)

var validSecretTypes = map[string]bool{
	"generic_secret": true,
	"api_key":        true,
	"access_token":   true,
	"password":       true,
	"client_secret":  true,
	"webhook_hmac":   true,
	"connection":     true,
}

type Store struct {
	db            storeDB
	begin         func(context.Context, *sql.TxOptions) (*sql.Tx, error)
	vault         *vault.Vault
	workspaceUUID string
}

type storeDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Item struct {
	ID                  int64          `json:"id"`
	Name                string         `json:"name"`
	OwnerProjectID      int64          `json:"owner_project_id"`
	OwnerProjectName    string         `json:"owner_project_name"`
	SecretType          string         `json:"secret_type"`
	ValueMode           string         `json:"value_mode"`
	ValueVersion        int64          `json:"value_version"`
	MetadataRevision    int64          `json:"metadata_revision"`
	EncryptionVersion   int            `json:"encryption_version"`
	Provider            string         `json:"provider"`
	Environment         string         `json:"environment"`
	Description         string         `json:"description"`
	ExpiresAt           string         `json:"expires_at,omitempty"`
	ExpiryWarningDays   int            `json:"expiry_warning_days"`
	LastValueReplacedAt string         `json:"last_value_replaced_at"`
	LastUsedAt          string         `json:"last_used_at,omitempty"`
	UsageCount          int64          `json:"usage_count"`
	Source              string         `json:"source"`
	GeneratorKind       string         `json:"generator_kind,omitempty"`
	GeneratorParameters map[string]any `json:"generator_parameters"`
	Status              string         `json:"status"`
	CreatedAt           string         `json:"created_at"`
	UpdatedAt           string         `json:"updated_at"`
	ProjectIDs          []int64        `json:"project_ids"`
	Tags                []string       `json:"tags"`
	UsageNotes          []UsageNote    `json:"usage_notes"`
}

type UsageNote struct {
	ID        int64  `json:"id"`
	Location  string `json:"location"`
	Notes     string `json:"notes"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CreateInput struct {
	Name              string
	Value             string
	OwnerProjectID    int64
	SharedProjectIDs  []int64
	SecretType        string
	Provider          string
	Environment       string
	Description       string
	ExpiresAt         string
	ExpiryWarningDays int
	Source            string
	GeneratorKind     string
	GeneratorParams   map[string]any
	Tags              []string
	UsageNotes        []UsageNote
}

type ListFilter struct {
	ProjectID int64
	Query     string
	Limit     int
	Offset    int
}

type ReplaceValueInput struct {
	ID                   int64
	Value                string
	Source               string
	GeneratorKind        string
	GeneratorParams      map[string]any
	ExpectedValueVersion int64
}

type UpdateMetadataInput struct {
	ID                       int64
	ExpectedMetadataRevision int64
	Name                     string
	OwnerProjectID           int64
	SharedProjectIDs         []int64
	SecretType               string
	Provider                 string
	Environment              string
	Description              string
	ExpiresAt                string
	ExpiryWarningDays        int
	Tags                     []string
	UsageNotes               []UsageNote
}

type encryptedItemValue struct {
	Value string `json:"value"`
}

func NewStore(db *sql.DB, secretVault *vault.Vault, workspaceUUID string) (*Store, error) {
	if db == nil || secretVault == nil {
		return nil, fmt.Errorf("database and secret vault are required")
	}
	workspaceUUID = strings.TrimSpace(workspaceUUID)
	if workspaceUUID == "" {
		return nil, fmt.Errorf("workspace UUID is required")
	}
	return &Store{db: db, begin: db.BeginTx, vault: secretVault, workspaceUUID: workspaceUUID}, nil
}

func (s *Store) WithTx(tx *sql.Tx) *Store {
	return &Store{db: tx, vault: s.vault, workspaceUUID: s.workspaceUUID}
}

func (s *Store) transaction(ctx context.Context, options *sql.TxOptions) (storeDB, func() error, func(), error) {
	if s.begin == nil {
		return s.db, func() error { return nil }, func() {}, nil
	}
	tx, err := s.begin(ctx, options)
	if err != nil {
		return nil, nil, nil, err
	}
	return tx, tx.Commit, func() { _ = tx.Rollback() }, nil
}

func EnsureWorkspaceUUID(ctx context.Context, db *sql.DB) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, workspaceUUIDSetting).Scan(&value)
	if err == nil && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read workspace UUID: %w", err)
	}
	value, err = randomUUID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO NOTHING`, workspaceUUIDSetting, value, now); err != nil {
		return "", fmt.Errorf("store workspace UUID: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, workspaceUUIDSetting).Scan(&value); err != nil {
		return "", fmt.Errorf("read stored workspace UUID: %w", err)
	}
	return strings.TrimSpace(value), nil
}
