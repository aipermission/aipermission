package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type Store struct {
	db sqldb.Executor
}

type storeDB = sqldb.Executor

type ValidationError string

func (e ValidationError) Error() string {
	return string(e)
}

var ErrRemoteCleanupPending = errors.New("connector target has pending remote transfer cleanup")

const targetMutationRemoteCleanupGuard = `
	AND NOT EXISTS (
		SELECT 1 FROM file_transfers ft
		JOIN connector_runtime_surfaces runtime ON runtime.id = ft.runtime_id
		WHERE runtime.target_id = connector_targets.id
		  AND (ft.remote_staging_ref != '' OR ft.status IN ('pending', 'pending_approval', 'running', 'paused'))
	)`

const profileMutationRemoteCleanupGuard = `
	AND NOT EXISTS (
		SELECT 1 FROM file_transfers ft
		JOIN connector_runtime_surfaces runtime ON runtime.id = ft.runtime_id
		WHERE runtime.target_id = connector_credential_profiles.target_id
		  AND runtime.profile_id = connector_credential_profiles.id
		  AND (ft.remote_staging_ref != '' OR ft.status IN ('pending', 'pending_approval', 'running', 'paused'))
	)`

func (s *Store) requireNoRemoteCleanup(ctx context.Context, targetID, profileID int64) error {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM file_transfers ft
			JOIN connector_runtime_surfaces runtime ON runtime.id = ft.runtime_id
			WHERE runtime.target_id = ?
			  AND (ft.remote_staging_ref != '' OR ft.status IN ('pending', 'pending_approval', 'running', 'paused'))`
	args := []any{targetID}
	if profileID > 0 {
		query += ` AND runtime.profile_id = ?`
		args = append(args, profileID)
	}
	query += `)`
	var pending bool
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&pending); err != nil {
		return fmt.Errorf("inspect connector remote cleanup state: %w", err)
	}
	if pending {
		return ErrRemoteCleanupPending
	}
	return nil
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func NewTxStore(tx *sql.Tx) *Store {
	return &Store{db: tx}
}

func (s *Store) transaction(ctx context.Context, label string) (sqldb.Executor, func() error, func(), error) {
	return sqldb.Transaction(ctx, s.db, nil, label)
}
