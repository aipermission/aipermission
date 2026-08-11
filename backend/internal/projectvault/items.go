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

	tx, commit, rollback, err := s.transaction(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin create vault item: %w", err)
	}
	defer rollback()
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
	item, err := (&Store{db: tx, vault: s.vault, workspaceUUID: s.workspaceUUID}).Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if err := commit(); err != nil {
		return Item{}, fmt.Errorf("commit vault item: %w", err)
	}
	return item, nil
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
