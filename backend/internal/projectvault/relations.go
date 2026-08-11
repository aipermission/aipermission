package projectvault

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

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

func validateActiveProjects(ctx context.Context, db storeDB, ids []int64) error {
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

func enforceCreateQuota(ctx context.Context, tx storeDB, ownerProjectID int64) error {
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

func insertAssignments(ctx context.Context, tx storeDB, itemID int64, ids []int64, now string) error {
	return insertAssignmentsAtRevision(ctx, tx, itemID, ids, 1, now)
}

func insertAssignmentsAtRevision(ctx context.Context, tx storeDB, itemID int64, ids []int64, revision int64, now string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vault_item_projects (vault_item_id, project_id, assignment_revision, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)`, itemID, id, revision, now, now); err != nil {
			return fmt.Errorf("update vault project assignment: %w", err)
		}
	}
	return nil
}

func insertTags(ctx context.Context, tx storeDB, itemID int64, tags []string, now string) error {
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO vault_item_tags (vault_item_id, tag, created_at) VALUES (?, ?, ?)`, itemID, tag, now); err != nil {
			return fmt.Errorf("insert vault tag: %w", err)
		}
	}
	return nil
}

func insertUsageNotes(ctx context.Context, tx storeDB, itemID int64, notes []UsageNote, now string) error {
	for _, note := range notes {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vault_item_usage_notes (vault_item_id, location, notes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)`, itemID, note.Location, note.Notes, now, now); err != nil {
			return fmt.Errorf("insert vault usage note: %w", err)
		}
	}
	return nil
}
