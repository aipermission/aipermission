package vaultfinalization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

var ErrPending = errors.New("Vault change was saved but session cleanup is pending; retry after reopening the database")

type Reference struct {
	SessionID  int64 `json:"session_id"`
	RuntimeID  int64 `json:"runtime_id"`
	Generation int64 `json:"generation"`
}

type Intent struct {
	ID         int64
	Kind       string
	ItemID     int64
	BindingID  int64
	ProjectID  int64
	References []Reference
	Reason     string
}

type Store struct{ db sqldb.Executor }

func NewStore(db sqldb.Executor) *Store { return &Store{db: db} }

func (s *Store) Queue(ctx context.Context, intent Intent) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("Vault finalization store is unavailable")
	}
	mutation := intent.Kind == "mutation" && (intent.ItemID > 0 || intent.BindingID > 0) && intent.ProjectID == 0
	project := intent.Kind == "project" && intent.ProjectID > 0 && intent.ItemID == 0 && intent.BindingID == 0
	if !mutation && !project {
		return 0, errors.New("invalid Vault finalization intent")
	}
	references := intent.References
	if references == nil {
		references = []Reference{}
	}
	payload, err := json.Marshal(references)
	if err != nil {
		return 0, fmt.Errorf("encode Vault finalization references: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO vault_finalizations
		(kind, item_id, binding_id, project_id, references_json, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, intent.Kind, intent.ItemID, intent.BindingID,
		intent.ProjectID, string(payload), intent.Reason, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("queue Vault finalization: %w", err)
	}
	return result.LastInsertId()
}

func (s *Store) Pending(ctx context.Context) ([]Intent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("Vault finalization store is unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, item_id, binding_id, project_id, references_json, reason FROM vault_finalizations ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list Vault finalizations: %w", err)
	}
	defer rows.Close()
	intents := make([]Intent, 0)
	for rows.Next() {
		var intent Intent
		var payload string
		if err := rows.Scan(&intent.ID, &intent.Kind, &intent.ItemID, &intent.BindingID, &intent.ProjectID, &payload, &intent.Reason); err != nil {
			return nil, fmt.Errorf("scan Vault finalization: %w", err)
		}
		if err := json.Unmarshal([]byte(payload), &intent.References); err != nil {
			return nil, fmt.Errorf("decode Vault finalization references: %w", err)
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Vault finalizations: %w", err)
	}
	return intents, nil
}

func (s *Store) Complete(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return errors.New("Vault finalization store is unavailable")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM vault_finalizations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("complete Vault finalization: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) RequireReady(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("Vault finalization store is unavailable")
	}
	var pending bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM vault_finalizations)`).Scan(&pending); err != nil {
		return fmt.Errorf("inspect Vault finalizations: %w", err)
	}
	if pending {
		return ErrPending
	}
	return nil
}
