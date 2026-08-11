package projectvault

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
	tx, commit, rollback, err := s.transaction(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin update vault metadata: %w", err)
	}
	defer rollback()
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
	item, err := (&Store{db: tx, vault: s.vault, workspaceUUID: s.workspaceUUID}).Get(ctx, input.ID)
	if err != nil {
		return Item{}, err
	}
	if err := commit(); err != nil {
		return Item{}, fmt.Errorf("commit vault metadata: %w", err)
	}
	return item, nil
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
