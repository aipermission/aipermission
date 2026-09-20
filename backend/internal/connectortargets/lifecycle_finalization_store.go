package connectortargets

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

var (
	ErrLifecycleFinalizationStoreUnavailable = errors.New("connector lifecycle finalization store is unavailable")
	ErrLifecycleFinalizationPending          = errors.New("connector lifecycle finalization is pending")
)

type LifecycleFinalizationChange struct {
	TargetID       int64
	ProfileID      int64
	StaleReason    string
	UserMessage    string
	IncludeRunning bool
}

type PendingLifecycleFinalization struct {
	ID              int64
	Change          LifecycleFinalizationChange
	VaultPending    bool
	RequestsPending bool
}

type LifecycleFinalizationStore struct{ database sqldb.Executor }

func NewLifecycleFinalizationStore(database sqldb.Executor) *LifecycleFinalizationStore {
	return &LifecycleFinalizationStore{database: database}
}

func (store *LifecycleFinalizationStore) Queue(ctx context.Context, change LifecycleFinalizationChange) error {
	if err := store.validate(); err != nil {
		return err
	}
	if change.TargetID < 1 || change.ProfileID < 0 {
		return fmt.Errorf("invalid connector lifecycle finalization scope")
	}
	includeRunning := 0
	if change.IncludeRunning {
		includeRunning = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := store.database.ExecContext(ctx, `
		INSERT INTO connector_lifecycle_finalizations (
			target_id, profile_id, stale_reason, user_message, include_running,
			vault_pending, requests_pending, attempt_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 1, 1, 0, ?, ?)
		ON CONFLICT(target_id, profile_id) DO UPDATE SET
			stale_reason = excluded.stale_reason,
			user_message = excluded.user_message,
			include_running = MAX(connector_lifecycle_finalizations.include_running, excluded.include_running),
			vault_pending = 1,
			requests_pending = 1,
			updated_at = excluded.updated_at`,
		change.TargetID, change.ProfileID, change.StaleReason, change.UserMessage,
		includeRunning, now, now,
	)
	if err != nil {
		return fmt.Errorf("queue connector lifecycle finalization: %w", err)
	}
	return nil
}

func (store *LifecycleFinalizationStore) Pending(ctx context.Context) ([]PendingLifecycleFinalization, error) {
	if err := store.validate(); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `
		SELECT id, target_id, profile_id, stale_reason, user_message, include_running,
			vault_pending, requests_pending
		FROM connector_lifecycle_finalizations
		WHERE vault_pending = 1 OR requests_pending = 1
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list connector lifecycle finalizations: %w", err)
	}
	defer rows.Close()
	items := make([]PendingLifecycleFinalization, 0)
	for rows.Next() {
		var item PendingLifecycleFinalization
		if err := rows.Scan(
			&item.ID, &item.Change.TargetID, &item.Change.ProfileID,
			&item.Change.StaleReason, &item.Change.UserMessage, &item.Change.IncludeRunning,
			&item.VaultPending, &item.RequestsPending,
		); err != nil {
			return nil, fmt.Errorf("scan connector lifecycle finalization: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list connector lifecycle finalizations: %w", err)
	}
	return items, nil
}

func (store *LifecycleFinalizationStore) MarkVaultComplete(ctx context.Context, id int64) error {
	return store.markComplete(ctx, id, "vault_pending")
}

func (store *LifecycleFinalizationStore) MarkRequestsComplete(ctx context.Context, id int64) error {
	return store.markComplete(ctx, id, "requests_pending")
}

func (store *LifecycleFinalizationStore) RecordAttempt(ctx context.Context, id int64) error {
	if err := store.validate(); err != nil {
		return err
	}
	result, err := store.database.ExecContext(ctx, `
		UPDATE connector_lifecycle_finalizations
		SET attempt_count = attempt_count + 1, updated_at = ?
		WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("record connector lifecycle finalization attempt: %w", err)
	}
	return requireOneRow(result, "connector lifecycle finalization")
}

func (store *LifecycleFinalizationStore) RequireReady(ctx context.Context) error {
	if err := store.validate(); err != nil {
		return err
	}
	var pending bool
	if err := store.database.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM connector_lifecycle_finalizations
			WHERE vault_pending = 1 OR requests_pending = 1
		)`).Scan(&pending); err != nil {
		return fmt.Errorf("inspect connector lifecycle finalizations: %w", err)
	}
	if pending {
		return ErrLifecycleFinalizationPending
	}
	return nil
}

func (store *LifecycleFinalizationStore) markComplete(ctx context.Context, id int64, column string) error {
	if err := store.validate(); err != nil {
		return err
	}
	if column != "vault_pending" && column != "requests_pending" {
		return fmt.Errorf("invalid connector lifecycle finalization component")
	}
	executor, commit, rollback, err := sqldb.Transaction(ctx, store.database, nil, "connector lifecycle finalization component")
	if err != nil {
		return err
	}
	defer rollback()
	result, err := executor.ExecContext(ctx, `
		UPDATE connector_lifecycle_finalizations
		SET `+column+` = 0, updated_at = ?
		WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("mark connector lifecycle finalization component complete: %w", err)
	}
	if err := requireOneRow(result, "connector lifecycle finalization"); err != nil {
		return err
	}
	if _, err := executor.ExecContext(ctx, `
		DELETE FROM connector_lifecycle_finalizations
		WHERE id = ? AND vault_pending = 0 AND requests_pending = 0`, id); err != nil {
		return fmt.Errorf("remove completed connector lifecycle finalization: %w", err)
	}
	return commit()
}

func (store *LifecycleFinalizationStore) validate() error {
	if store == nil || store.database == nil {
		return ErrLifecycleFinalizationStoreUnavailable
	}
	return nil
}

type rowsAffected interface{ RowsAffected() (int64, error) }

func requireOneRow(result rowsAffected, label string) error {
	if result == nil {
		return fmt.Errorf("%s update returned no result", label)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("%s was not found", label)
	}
	return nil
}
