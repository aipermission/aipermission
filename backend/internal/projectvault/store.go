package projectvault

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
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
	db            *sql.DB
	vault         *vault.Vault
	workspaceUUID string
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
	return &Store{db: db, vault: secretVault, workspaceUUID: workspaceUUID}, nil
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

func (s *Store) Create(ctx context.Context, input CreateInput) (Item, error) {
	normalized, err := s.normalizeCreateInput(ctx, input)
	if err != nil {
		return Item{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	generatorJSON, err := json.Marshal(normalized.GeneratorParams)
	if err != nil {
		return Item{}, ValidationError("invalid generator parameters")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin create vault item: %w", err)
	}
	defer tx.Rollback()
	if err := enforceCreateQuota(ctx, tx, normalized.OwnerProjectID); err != nil {
		return Item{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO vault_items (
			name, owner_project_id, secret_type, value_mode, value_version, metadata_revision,
			encryption_version, provider, environment, description, expires_at, expiry_warning_days,
			last_value_replaced_at, source, generator_kind, generator_parameters_json, status,
			created_at, updated_at
		)
		VALUES (?, ?, ?, 'text', 1, 1, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, 'active', ?, ?)`,
		normalized.Name, normalized.OwnerProjectID, normalized.SecretType, itemEncryptionVersion,
		normalized.Provider, normalized.Environment, normalized.Description, normalized.ExpiresAt,
		normalized.ExpiryWarningDays, now, normalized.Source, normalized.GeneratorKind,
		string(generatorJSON), now, now,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Item{}, ValidationError("an active vault item with this name already exists")
		}
		return Item{}, fmt.Errorf("insert vault item: %w", err)
	}
	itemID, err := result.LastInsertId()
	if err != nil {
		return Item{}, fmt.Errorf("read vault item id: %w", err)
	}
	aad, err := itemAssociatedData(s.workspaceUUID, itemID, 1, itemEncryptionVersion)
	if err != nil {
		return Item{}, err
	}
	encrypted, err := s.vault.EncryptJSONWithAAD(encryptedItemValue{Value: normalized.Value}, aad)
	if err != nil {
		return Item{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE vault_items SET encrypted_value = ? WHERE id = ?`, encrypted, itemID); err != nil {
		return Item{}, fmt.Errorf("store encrypted vault value: %w", err)
	}
	if err := insertAssignments(ctx, tx, itemID, normalized.SharedProjectIDs, now); err != nil {
		return Item{}, err
	}
	if err := insertTags(ctx, tx, itemID, normalized.Tags, now); err != nil {
		return Item{}, err
	}
	if err := insertUsageNotes(ctx, tx, itemID, normalized.UsageNotes, now); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit vault item: %w", err)
	}
	return s.Get(ctx, itemID)
}

func (s *Store) Get(ctx context.Context, id int64) (Item, error) {
	item, err := scanItem(s.db.QueryRowContext(ctx, itemSelect+` WHERE vi.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("get vault item: %w", err)
	}
	if err := s.loadRelations(ctx, &item); err != nil {
		return Item{}, err
	}
	return item, nil
}

func (s *Store) List(ctx context.Context, filter ListFilter) ([]Item, int, error) {
	if filter.Limit < 1 || filter.Limit > maxMetadataListPage {
		filter.Limit = maxMetadataListPage
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	query := strings.TrimSpace(filter.Query)
	where := []string{"vi.status = 'active'"}
	args := []any{}
	if filter.ProjectID > 0 {
		where = append(where, `(vi.owner_project_id = ? OR EXISTS (
			SELECT 1 FROM vault_item_projects vip WHERE vip.vault_item_id = vi.id AND vip.project_id = ?
		))`)
		args = append(args, filter.ProjectID, filter.ProjectID)
	}
	if query != "" {
		like := "%" + query + "%"
		where = append(where, `(vi.name LIKE ? OR vi.provider LIKE ? OR vi.environment LIKE ? OR vi.description LIKE ?)`)
		args = append(args, like, like, like, like)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vault_items vi WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count vault items: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, itemSelect+` WHERE `+whereSQL+`
		ORDER BY lower(vi.name), vi.id
		LIMIT ? OFFSET ?`, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list vault items: %w", err)
	}
	items := []Item{}
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	for index := range items {
		if err := s.loadRelations(ctx, &items[index]); err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func (s *Store) Reveal(ctx context.Context, id int64) (string, error) {
	var encrypted string
	var valueVersion int64
	var encryptionVersion int
	err := s.db.QueryRowContext(ctx, `
		SELECT encrypted_value, value_version, encryption_version
		FROM vault_items WHERE id = ? AND status = 'active'`, id).
		Scan(&encrypted, &valueVersion, &encryptionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read encrypted vault item: %w", err)
	}
	aad, err := itemAssociatedData(s.workspaceUUID, id, valueVersion, encryptionVersion)
	if err != nil {
		return "", err
	}
	var decoded encryptedItemValue
	if err := s.vault.DecryptJSONWithAAD(encrypted, &decoded, aad); err != nil {
		return "", fmt.Errorf("decrypt vault item: %w", err)
	}
	return decoded.Value, nil
}

func (s *Store) ReplaceValue(ctx context.Context, input ReplaceValueInput) (Item, error) {
	input.Source = strings.TrimSpace(input.Source)
	input.GeneratorKind = strings.TrimSpace(input.GeneratorKind)
	if input.Source == "" {
		input.Source = "imported"
	}
	var generatorParameters map[string]any
	switch input.Source {
	case "generated":
		if input.GeneratorKind == "" {
			return Item{}, ValidationError("generator kind is required")
		}
		if err := ValidateGeneratorKind(input.GeneratorKind); err != nil {
			return Item{}, err
		}
		if input.Value == "" || len(input.GeneratorParams) == 0 {
			return Item{}, ValidationError("generated replacement preview is required")
		}
		generatorParameters = input.GeneratorParams
	case "imported":
		if input.GeneratorKind != "" {
			return Item{}, ValidationError("imported values cannot specify a generator")
		}
		generatorParameters = map[string]any{}
	default:
		return Item{}, ValidationError("source must be imported or generated")
	}
	if err := validateValue(input.Value); err != nil {
		return Item{}, err
	}
	if input.ID < 1 || input.ExpectedValueVersion < 1 {
		return Item{}, ValidationError("item id and expected value version are required")
	}
	nextVersion := input.ExpectedValueVersion + 1
	aad, err := itemAssociatedData(s.workspaceUUID, input.ID, nextVersion, itemEncryptionVersion)
	if err != nil {
		return Item{}, err
	}
	encrypted, err := s.vault.EncryptJSONWithAAD(encryptedItemValue{Value: input.Value}, aad)
	if err != nil {
		return Item{}, err
	}
	generatorJSON, err := json.Marshal(generatorParameters)
	if err != nil {
		return Item{}, ValidationError("invalid generator parameters")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		UPDATE vault_items
		SET encrypted_value = ?, value_version = ?, encryption_version = ?,
			last_value_replaced_at = ?, source = ?, generator_kind = ?,
			generator_parameters_json = ?, updated_at = ?
		WHERE id = ? AND status = 'active' AND value_version = ?`,
		encrypted, nextVersion, itemEncryptionVersion, now, input.Source, input.GeneratorKind,
		string(generatorJSON), now, input.ID, input.ExpectedValueVersion,
	)
	if err != nil {
		return Item{}, fmt.Errorf("replace vault value: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		if _, err := s.Get(ctx, input.ID); errors.Is(err, ErrNotFound) {
			return Item{}, ErrNotFound
		}
		return Item{}, ErrStale
	}
	return s.Get(ctx, input.ID)
}

func (s *Store) UpdateMetadata(ctx context.Context, input UpdateMetadataInput) (Item, error) {
	if input.ID < 1 || input.ExpectedMetadataRevision < 1 {
		return Item{}, ValidationError("item id and expected metadata revision are required")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.SecretType = strings.TrimSpace(input.SecretType)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Environment = strings.TrimSpace(input.Environment)
	input.Description = strings.TrimSpace(input.Description)
	if err := validateEnvironmentName(input.Name); err != nil {
		return Item{}, err
	}
	if !validSecretTypes[input.SecretType] {
		return Item{}, ValidationError("unsupported secret type")
	}
	if input.OwnerProjectID < 1 {
		return Item{}, ValidationError("owner project is required")
	}
	expiresAt, err := normalizeExpiry(input.ExpiresAt)
	if err != nil {
		return Item{}, err
	}
	input.ExpiresAt = expiresAt
	if input.ExpiryWarningDays < 1 || input.ExpiryWarningDays > 3650 {
		return Item{}, ValidationError("expiry warning days must be between 1 and 3650")
	}
	if err := validateMetadata(input.Provider, input.Environment, input.Description); err != nil {
		return Item{}, err
	}
	input.SharedProjectIDs, err = normalizeProjectIDs(input.OwnerProjectID, input.SharedProjectIDs)
	if err != nil {
		return Item{}, err
	}
	input.Tags, err = normalizeTags(input.Tags)
	if err != nil {
		return Item{}, err
	}
	input.UsageNotes, err = normalizeUsageNotes(input.UsageNotes)
	if err != nil {
		return Item{}, err
	}
	if err := validateActiveProjects(ctx, s.db, append([]int64{input.OwnerProjectID}, input.SharedProjectIDs...)); err != nil {
		return Item{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin update vault metadata: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE vault_items
		SET name = ?, owner_project_id = ?, secret_type = ?, provider = ?, environment = ?,
			description = ?, expires_at = NULLIF(?, ''), expiry_warning_days = ?,
			metadata_revision = metadata_revision + 1, updated_at = ?
		WHERE id = ? AND status = 'active' AND metadata_revision = ?`,
		input.Name, input.OwnerProjectID, input.SecretType, input.Provider, input.Environment,
		input.Description, input.ExpiresAt, input.ExpiryWarningDays, now,
		input.ID, input.ExpectedMetadataRevision,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Item{}, ValidationError("an active vault item with this name already exists")
		}
		return Item{}, fmt.Errorf("update vault metadata: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM vault_items WHERE id = ? AND status = 'active'`, input.ID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return Item{}, ErrNotFound
		} else if err != nil {
			return Item{}, err
		}
		return Item{}, ErrStale
	}
	var nextAssignmentRevision int64
	if err := tx.QueryRowContext(ctx, `
		SELECT metadata_revision
		FROM vault_items
		WHERE id = ?`, input.ID).Scan(&nextAssignmentRevision); err != nil {
		return Item{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vault_item_projects WHERE vault_item_id = ?`, input.ID); err != nil {
		return Item{}, err
	}
	if err := insertAssignmentsAtRevision(ctx, tx, input.ID, input.SharedProjectIDs, nextAssignmentRevision, now); err != nil {
		return Item{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vault_item_tags WHERE vault_item_id = ?`, input.ID); err != nil {
		return Item{}, err
	}
	if err := insertTags(ctx, tx, input.ID, input.Tags, now); err != nil {
		return Item{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vault_item_usage_notes WHERE vault_item_id = ?`, input.ID); err != nil {
		return Item{}, err
	}
	if err := insertUsageNotes(ctx, tx, input.ID, input.UsageNotes, now); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit vault metadata: %w", err)
	}
	return s.Get(ctx, input.ID)
}

func (s *Store) Delete(ctx context.Context, id int64, expectedValueVersion int64, expectedMetadataRevision int64) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_items
		WHERE id = ? AND value_version = ? AND metadata_revision = ?`,
		id, expectedValueVersion, expectedMetadataRevision,
	)
	if err != nil {
		return fmt.Errorf("delete vault item: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		if _, err := s.Get(ctx, id); errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return ErrStale
	}
	return nil
}

func (s *Store) normalizeCreateInput(ctx context.Context, input CreateInput) (CreateInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.SecretType = strings.TrimSpace(input.SecretType)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Environment = strings.TrimSpace(input.Environment)
	input.Description = strings.TrimSpace(input.Description)
	input.Source = strings.TrimSpace(input.Source)
	input.GeneratorKind = strings.TrimSpace(input.GeneratorKind)
	if err := validateEnvironmentName(input.Name); err != nil {
		return CreateInput{}, err
	}
	if !validSecretTypes[input.SecretType] {
		return CreateInput{}, ValidationError("unsupported secret type")
	}
	if input.OwnerProjectID < 1 {
		return CreateInput{}, ValidationError("owner project is required")
	}
	if input.Source != "imported" && input.Source != "generated" {
		return CreateInput{}, ValidationError("source must be imported or generated")
	}
	if input.Source == "generated" {
		if input.GeneratorKind == "" {
			return CreateInput{}, ValidationError("generator kind is required")
		}
		value, parameters, err := Generate(input.GeneratorKind)
		if err != nil {
			return CreateInput{}, err
		}
		input.Value = value
		input.GeneratorParams = parameters
	} else if input.GeneratorKind != "" {
		return CreateInput{}, ValidationError("imported values cannot specify a generator")
	}
	if err := validateValue(input.Value); err != nil {
		return CreateInput{}, err
	}
	expiresAt, err := normalizeExpiry(input.ExpiresAt)
	if err != nil {
		return CreateInput{}, err
	}
	input.ExpiresAt = expiresAt
	if input.ExpiryWarningDays == 0 {
		input.ExpiryWarningDays = defaultExpiryWarnDays
	}
	if input.ExpiryWarningDays < 1 || input.ExpiryWarningDays > 3650 {
		return CreateInput{}, ValidationError("expiry warning days must be between 1 and 3650")
	}
	if err := validateMetadata(input.Provider, input.Environment, input.Description); err != nil {
		return CreateInput{}, err
	}
	input.SharedProjectIDs, err = normalizeProjectIDs(input.OwnerProjectID, input.SharedProjectIDs)
	if err != nil {
		return CreateInput{}, err
	}
	input.Tags, err = normalizeTags(input.Tags)
	if err != nil {
		return CreateInput{}, err
	}
	input.UsageNotes, err = normalizeUsageNotes(input.UsageNotes)
	if err != nil {
		return CreateInput{}, err
	}
	if err := validateActiveProjects(ctx, s.db, append([]int64{input.OwnerProjectID}, input.SharedProjectIDs...)); err != nil {
		return CreateInput{}, err
	}
	return input, nil
}

const itemSelect = `
	SELECT vi.id, vi.name, vi.owner_project_id, p.name, vi.secret_type, vi.value_mode,
		vi.value_version, vi.metadata_revision, vi.encryption_version, vi.provider,
		vi.environment, vi.description, COALESCE(vi.expires_at, ''), vi.expiry_warning_days,
		vi.last_value_replaced_at, COALESCE(vi.last_used_at, ''), vi.usage_count, vi.source,
		vi.generator_kind, vi.generator_parameters_json, vi.status, vi.created_at, vi.updated_at
	FROM vault_items vi
	JOIN projects p ON p.id = vi.owner_project_id`

func scanItem(scanner interface{ Scan(...any) error }) (Item, error) {
	var item Item
	var generatorJSON string
	err := scanner.Scan(
		&item.ID, &item.Name, &item.OwnerProjectID, &item.OwnerProjectName, &item.SecretType,
		&item.ValueMode, &item.ValueVersion, &item.MetadataRevision, &item.EncryptionVersion,
		&item.Provider, &item.Environment, &item.Description, &item.ExpiresAt,
		&item.ExpiryWarningDays, &item.LastValueReplacedAt, &item.LastUsedAt, &item.UsageCount,
		&item.Source, &item.GeneratorKind, &generatorJSON, &item.Status, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return Item{}, err
	}
	item.GeneratorParameters = map[string]any{}
	if err := json.Unmarshal([]byte(generatorJSON), &item.GeneratorParameters); err != nil {
		return Item{}, fmt.Errorf("decode vault generator parameters: %w", err)
	}
	return item, nil
}

func (s *Store) loadRelations(ctx context.Context, item *Item) error {
	rows, err := s.db.QueryContext(ctx, `SELECT project_id FROM vault_item_projects WHERE vault_item_id = ? ORDER BY project_id`, item.ID)
	if err != nil {
		return err
	}
	item.ProjectIDs = []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		item.ProjectIDs = append(item.ProjectIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tagRows, err := s.db.QueryContext(ctx, `SELECT tag FROM vault_item_tags WHERE vault_item_id = ? ORDER BY lower(tag)`, item.ID)
	if err != nil {
		return err
	}
	item.Tags = []string{}
	for tagRows.Next() {
		var tag string
		if err := tagRows.Scan(&tag); err != nil {
			tagRows.Close()
			return err
		}
		item.Tags = append(item.Tags, tag)
	}
	if err := tagRows.Err(); err != nil {
		tagRows.Close()
		return err
	}
	if err := tagRows.Close(); err != nil {
		return err
	}
	noteRows, err := s.db.QueryContext(ctx, `
		SELECT id, location, notes, created_at, updated_at
		FROM vault_item_usage_notes WHERE vault_item_id = ? ORDER BY id`, item.ID)
	if err != nil {
		return err
	}
	item.UsageNotes = []UsageNote{}
	for noteRows.Next() {
		var note UsageNote
		if err := noteRows.Scan(&note.ID, &note.Location, &note.Notes, &note.CreatedAt, &note.UpdatedAt); err != nil {
			noteRows.Close()
			return err
		}
		item.UsageNotes = append(item.UsageNotes, note)
	}
	if err := noteRows.Err(); err != nil {
		noteRows.Close()
		return err
	}
	return noteRows.Close()
}

func validateEnvironmentName(value string) error {
	if err := sessionenv.ValidateName(value); err != nil {
		return ValidationError(err.Error())
	}
	return nil
}

func validateValue(value string) error {
	if value == "" {
		return ValidationError("value is required")
	}
	if len(value) > maxValueBytes {
		return ValidationError("value exceeds the 16 KiB limit")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return ValidationError("text values cannot contain NUL bytes")
	}
	if !utf8.ValidString(value) {
		return ValidationError("text values must be valid UTF-8")
	}
	return nil
}

func validateMetadata(values ...string) error {
	for _, value := range values {
		if utf8.RuneCountInString(value) > maxMetadataRunes {
			return ValidationError("metadata field is too long")
		}
		for _, r := range value {
			if !unicode.IsPrint(r) && r != '\n' && r != '\t' {
				return ValidationError("metadata contains unsupported characters")
			}
		}
	}
	return nil
}

func normalizeExpiry(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || !parsed.After(time.Now().UTC()) {
		return "", ValidationError("expires_at must be a future RFC3339 timestamp")
	}
	return parsed.UTC().Format(time.RFC3339), nil
}

func normalizeProjectIDs(ownerID int64, values []int64) ([]int64, error) {
	if len(values)+1 > maxProjectsPerItem {
		return nil, ValidationError("too many shared projects")
	}
	seen := map[int64]bool{ownerID: true}
	output := make([]int64, 0, len(values))
	for _, id := range values {
		if id < 1 || seen[id] {
			return nil, ValidationError("shared project ids must be unique and exclude the owner")
		}
		seen[id] = true
		output = append(output, id)
	}
	return output, nil
}

func normalizeTags(values []string) ([]string, error) {
	if len(values) > maxTagsPerItem {
		return nil, ValidationError("too many tags")
	}
	seen := map[string]bool{}
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || utf8.RuneCountInString(value) > 64 || seen[key] {
			return nil, ValidationError("tags must be unique, non-empty, and 64 characters or fewer")
		}
		seen[key] = true
		output = append(output, value)
	}
	return output, nil
}

func normalizeUsageNotes(values []UsageNote) ([]UsageNote, error) {
	if len(values) > maxUsageNotesPerItem {
		return nil, ValidationError("too many usage notes")
	}
	for index := range values {
		values[index].Location = strings.TrimSpace(values[index].Location)
		values[index].Notes = strings.TrimSpace(values[index].Notes)
		if values[index].Location == "" {
			return nil, ValidationError("usage note location is required")
		}
		if err := validateMetadata(values[index].Location, values[index].Notes); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func validateActiveProjects(ctx context.Context, db *sql.DB, ids []int64) error {
	for _, id := range ids {
		var exists int
		if err := db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ? AND status = 'active'`, id).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ValidationError("project not found or archived")
			}
			return err
		}
	}
	return nil
}

func enforceCreateQuota(ctx context.Context, tx *sql.Tx, ownerProjectID int64) error {
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM vault_items WHERE status = 'active'`).Scan(&total); err != nil {
		return err
	}
	if total >= maxItemsPerDatabase {
		return ValidationError("vault item database quota reached")
	}
	var ownerTotal int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM vault_items WHERE status = 'active' AND owner_project_id = ?`, ownerProjectID).Scan(&ownerTotal); err != nil {
		return err
	}
	if ownerTotal >= maxItemsPerOwner {
		return ValidationError("vault item project quota reached")
	}
	return nil
}

func insertAssignments(ctx context.Context, tx *sql.Tx, itemID int64, ids []int64, now string) error {
	return insertAssignmentsAtRevision(ctx, tx, itemID, ids, 1, now)
}

func insertAssignmentsAtRevision(ctx context.Context, tx *sql.Tx, itemID int64, ids []int64, revision int64, now string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vault_item_projects (vault_item_id, project_id, assignment_revision, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)`, itemID, id, revision, now, now); err != nil {
			return fmt.Errorf("update vault project assignment: %w", err)
		}
	}
	return nil
}

func insertTags(ctx context.Context, tx *sql.Tx, itemID int64, tags []string, now string) error {
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO vault_item_tags (vault_item_id, tag, created_at) VALUES (?, ?, ?)`, itemID, tag, now); err != nil {
			return fmt.Errorf("insert vault tag: %w", err)
		}
	}
	return nil
}

func insertUsageNotes(ctx context.Context, tx *sql.Tx, itemID int64, notes []UsageNote, now string) error {
	for _, note := range notes {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vault_item_usage_notes (vault_item_id, location, notes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)`, itemID, note.Location, note.Notes, now, now); err != nil {
			return fmt.Errorf("insert vault usage note: %w", err)
		}
	}
	return nil
}
