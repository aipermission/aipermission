package projectvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type DefaultBinding struct {
	ID                int64  `json:"id"`
	VaultItemID       int64  `json:"vault_item_id"`
	VaultItemName     string `json:"vault_item_name"`
	SourceProjectID   int64  `json:"source_project_id"`
	SourceProjectName string `json:"source_project_name"`
	TargetID          int64  `json:"target_id"`
	TargetName        string `json:"target_name"`
	ConnectorKind     string `json:"connector_kind"`
	ProfileID         int64  `json:"profile_id"`
	ProfileLabel      string `json:"profile_label"`
	ReplaceExisting   bool   `json:"replace_existing"`
	BindingRevision   int64  `json:"binding_revision"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type DefaultBindingInput struct {
	VaultItemID             int64
	SourceProjectID         int64
	TargetID                int64
	ProfileID               int64
	ReplaceExisting         bool
	ExpectedBindingRevision int64
}

const defaultBindingSelect = `
	SELECT b.id, b.vault_item_id, vi.name, b.source_project_id, p.name,
		b.target_id, t.name, t.connector_kind, b.profile_id, cp.label,
		b.replace_existing, b.binding_revision, b.created_at, b.updated_at
	FROM vault_default_bindings b
	JOIN vault_items vi ON vi.id = b.vault_item_id
	JOIN projects p ON p.id = b.source_project_id
	JOIN connector_targets t ON t.id = b.target_id
	JOIN connector_credential_profiles cp ON cp.id = b.profile_id AND cp.target_id = b.target_id`

func (s *Store) ListDefaultBindings(ctx context.Context, vaultItemID, targetID, profileID int64) ([]DefaultBinding, error) {
	where := " WHERE 1 = 1"
	args := []any{}
	if vaultItemID > 0 {
		where += " AND b.vault_item_id = ?"
		args = append(args, vaultItemID)
	}
	if targetID > 0 {
		where += " AND b.target_id = ?"
		args = append(args, targetID)
	}
	if profileID > 0 {
		where += " AND b.profile_id = ?"
		args = append(args, profileID)
	}
	rows, err := s.db.QueryContext(ctx, defaultBindingSelect+where+`
		ORDER BY lower(t.name), lower(cp.label), lower(vi.name), b.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list vault default bindings: %w", err)
	}
	defer rows.Close()
	items := []DefaultBinding{}
	for rows.Next() {
		item, err := scanDefaultBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vault default bindings: %w", err)
	}
	return items, nil
}

func (s *Store) SaveDefaultBinding(ctx context.Context, input DefaultBindingInput) (DefaultBinding, error) {
	if input.VaultItemID < 1 || input.SourceProjectID < 1 || input.TargetID < 1 || input.ProfileID < 1 {
		return DefaultBinding{}, ValidationError("vault item, source project, target, and profile are required")
	}
	if input.ExpectedBindingRevision < 0 {
		return DefaultBinding{}, ValidationError("expected binding revision cannot be negative")
	}
	tx, commit, rollback, err := s.transaction(ctx, nil)
	if err != nil {
		return DefaultBinding{}, fmt.Errorf("begin save vault default binding: %w", err)
	}
	defer rollback()
	if err := validateDefaultBindingReferences(ctx, tx, input); err != nil {
		return DefaultBinding{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var id int64
	var currentRevision int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, binding_revision
		FROM vault_default_bindings
		WHERE vault_item_id = ? AND source_project_id = ? AND target_id = ? AND profile_id = ?`,
		input.VaultItemID, input.SourceProjectID, input.TargetID, input.ProfileID,
	).Scan(&id, &currentRevision)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if input.ExpectedBindingRevision != 0 {
			return DefaultBinding{}, ErrStale
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO vault_default_bindings (
				vault_item_id, source_project_id, target_id, profile_id,
				replace_existing, binding_revision, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
			input.VaultItemID, input.SourceProjectID, input.TargetID, input.ProfileID,
			input.ReplaceExisting, now, now,
		)
		if err != nil {
			return DefaultBinding{}, fmt.Errorf("create vault default binding: %w", err)
		}
		id, err = result.LastInsertId()
		if err != nil {
			return DefaultBinding{}, fmt.Errorf("read vault default binding id: %w", err)
		}
	case err != nil:
		return DefaultBinding{}, fmt.Errorf("read vault default binding revision: %w", err)
	default:
		if input.ExpectedBindingRevision != currentRevision {
			return DefaultBinding{}, ErrStale
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE vault_default_bindings
			SET replace_existing = ?, binding_revision = binding_revision + 1, updated_at = ?
			WHERE id = ? AND binding_revision = ?`,
			input.ReplaceExisting, now, id, input.ExpectedBindingRevision,
		)
		if err != nil {
			return DefaultBinding{}, fmt.Errorf("update vault default binding: %w", err)
		}
		affected, err := sqldb.RowsAffected(result, "update Vault default binding")
		if err != nil {
			return DefaultBinding{}, err
		}
		if affected != 1 {
			return DefaultBinding{}, ErrStale
		}
	}
	item, err := (&Store{db: tx, vault: s.vault, workspaceUUID: s.workspaceUUID}).getDefaultBinding(ctx, id)
	if err != nil {
		return DefaultBinding{}, err
	}
	if err := commit(); err != nil {
		return DefaultBinding{}, fmt.Errorf("commit vault default binding: %w", err)
	}
	return item, nil
}

func (s *Store) DeleteDefaultBinding(ctx context.Context, id, expectedRevision int64) error {
	if id < 1 || expectedRevision < 1 {
		return ValidationError("binding id and expected binding revision are required")
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_default_bindings
		WHERE id = ? AND binding_revision = ?`, id, expectedRevision)
	if err != nil {
		return fmt.Errorf("delete vault default binding: %w", err)
	}
	affected, err := sqldb.RowsAffected(result, "delete Vault default binding")
	if err != nil {
		return err
	}
	if affected == 1 {
		return nil
	}
	var exists int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM vault_default_bindings WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check vault default binding: %w", err)
	}
	return ErrStale
}

func (s *Store) getDefaultBinding(ctx context.Context, id int64) (DefaultBinding, error) {
	item, err := scanDefaultBinding(s.db.QueryRowContext(ctx, defaultBindingSelect+` WHERE b.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultBinding{}, ErrNotFound
	}
	if err != nil {
		return DefaultBinding{}, fmt.Errorf("get vault default binding: %w", err)
	}
	return item, nil
}

func (s *Store) GetDefaultBinding(ctx context.Context, id int64) (DefaultBinding, error) {
	return s.getDefaultBinding(ctx, id)
}

func (s *Store) FindDefaultBinding(ctx context.Context, input DefaultBindingInput) (DefaultBinding, bool, error) {
	item, err := scanDefaultBinding(s.db.QueryRowContext(ctx, defaultBindingSelect+`
		WHERE b.vault_item_id = ? AND b.source_project_id = ?
		  AND b.target_id = ? AND b.profile_id = ?`,
		input.VaultItemID, input.SourceProjectID, input.TargetID, input.ProfileID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultBinding{}, false, nil
	}
	if err != nil {
		return DefaultBinding{}, false, fmt.Errorf("find vault default binding: %w", err)
	}
	return item, true, nil
}

func validateDefaultBindingReferences(ctx context.Context, tx storeDB, input DefaultBindingInput) error {
	var itemActive, projectActive, assigned, targetActive, profileActive int
	err := tx.QueryRowContext(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM vault_items WHERE id = ? AND status = 'active'),
			EXISTS(SELECT 1 FROM projects WHERE id = ? AND status = 'active'),
			EXISTS(
				SELECT 1 FROM vault_items vi
				WHERE vi.id = ? AND vi.status = 'active'
				AND (
					vi.owner_project_id = ?
					OR EXISTS(
						SELECT 1 FROM vault_item_projects vip
						WHERE vip.vault_item_id = vi.id AND vip.project_id = ?
					)
				)
			),
			EXISTS(SELECT 1 FROM connector_targets WHERE id = ? AND status = 'active'),
			EXISTS(
				SELECT 1 FROM connector_credential_profiles
				WHERE id = ? AND target_id = ? AND status = 'active'
			)`,
		input.VaultItemID,
		input.SourceProjectID,
		input.VaultItemID, input.SourceProjectID, input.SourceProjectID,
		input.TargetID,
		input.ProfileID, input.TargetID,
	).Scan(&itemActive, &projectActive, &assigned, &targetActive, &profileActive)
	if err != nil {
		return fmt.Errorf("validate vault default binding references: %w", err)
	}
	switch {
	case itemActive == 0:
		return ValidationError("vault item is not active")
	case projectActive == 0:
		return ValidationError("source project is not active")
	case assigned == 0:
		return ValidationError("vault item is not assigned to the source project")
	case targetActive == 0:
		return ValidationError("connector target is not active")
	case profileActive == 0:
		return ValidationError("credential profile is not active for this target")
	default:
		return nil
	}
}

type bindingScanner interface{ Scan(...any) error }

func scanDefaultBinding(row bindingScanner) (DefaultBinding, error) {
	var item DefaultBinding
	var replaceExisting int
	err := row.Scan(
		&item.ID, &item.VaultItemID, &item.VaultItemName,
		&item.SourceProjectID, &item.SourceProjectName,
		&item.TargetID, &item.TargetName, &item.ConnectorKind,
		&item.ProfileID, &item.ProfileLabel,
		&replaceExisting, &item.BindingRevision,
		&item.CreatedAt, &item.UpdatedAt,
	)
	item.ReplaceExisting = replaceExisting == 1
	return item, err
}
