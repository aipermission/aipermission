package projectvault

import (
	"context"
	"fmt"
	"time"
)

type SessionReference struct {
	SessionID  int64
	RuntimeID  int64
	Generation int64
}

type SessionMutationScope struct {
	ItemID    int64
	BindingID int64
}

func (s *Store) RecordSessionItems(ctx context.Context, sessionID int64, items []SessionItem) error {
	if sessionID < 1 {
		return ValidationError("session id must be a positive integer")
	}
	tx, commit, rollback, err := s.transaction(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Vault session item tracking: %w", err)
	}
	defer rollback()
	if err := requireTrackedSession(ctx, tx, sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vault_session_items WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("clear Vault session items: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vault_session_items (
				session_id, vault_item_id, vault_item_name, source_project_id, value_version,
				metadata_revision, binding_id, binding_revision, created_at
			) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?)`,
			sessionID, item.ItemID, item.Name, item.SourceProjectID, item.ValueVersion,
			item.MetadataRevision, item.BindingID, item.BindingRevision, now,
		); err != nil {
			return fmt.Errorf("record Vault session item: %w", err)
		}
	}
	return commit()
}

func (s *Store) ActiveSessionsForMutation(ctx context.Context, scope SessionMutationScope) ([]SessionReference, error) {
	if scope.ItemID < 1 && scope.BindingID < 1 {
		return nil, ValidationError("Vault item or binding id is required")
	}
	where := "vsi.vault_item_id = ?"
	value := scope.ItemID
	if scope.BindingID > 0 {
		where = "vsi.binding_id = ?"
		value = scope.BindingID
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT cs.id, cs.runtime_id, cs.generation
		FROM vault_session_items vsi
		JOIN console_sessions cs ON cs.id = vsi.session_id
		WHERE `+where+` AND cs.status IN ('connecting', 'connected')
		ORDER BY cs.id`, value)
	if err != nil {
		return nil, fmt.Errorf("list affected Vault sessions: %w", err)
	}
	defer rows.Close()
	items := []SessionReference{}
	for rows.Next() {
		var item SessionReference
		if err := rows.Scan(&item.SessionID, &item.RuntimeID, &item.Generation); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func requireTrackedSession(ctx context.Context, tx storeDB, sessionID int64) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM console_sessions WHERE id = ?)`, sessionID).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return ErrNotFound
	}
	return nil
}
