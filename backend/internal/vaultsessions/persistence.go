package vaultsessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

var ErrPersistenceUnavailable = errors.New("Vault session lease persistence is unavailable")

type Executor = sqldb.Executor

type Persistence struct{ database sqldb.Executor }

type Reference struct {
	SessionID  int64
	RuntimeID  int64
	Generation int64
}

func NewPersistence(database Executor) *Persistence {
	return &Persistence{database: database}
}

func (p *Persistence) Grant(ctx context.Context, projectID int64, lease Lease) error {
	if p == nil || p.database == nil {
		return ErrPersistenceUnavailable
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := p.database.ExecContext(ctx, `
		INSERT INTO vault_session_leases (
			token_id, project_id, runtime_id, session_id, session_generation, approval_context_hash,
			environment_content_hash, status, expires_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)
		ON CONFLICT(token_id, runtime_id, session_id, session_generation, approval_context_hash)
		DO UPDATE SET project_id = excluded.project_id,
			environment_content_hash = excluded.environment_content_hash,
			status = 'active', expires_at = excluded.expires_at, updated_at = excluded.updated_at`,
		lease.TokenID, projectID, lease.RuntimeID, lease.SessionID, lease.SessionGeneration,
		lease.ApprovalContextHash, lease.EnvironmentContentHash,
		lease.ExpiresAt.UTC().Format(time.RFC3339), now, now,
	)
	return err
}

func (p *Persistence) Revoke(ctx context.Context, sessionID, generation int64) error {
	if p == nil || p.database == nil {
		return ErrPersistenceUnavailable
	}
	_, err := p.database.ExecContext(ctx, `
		UPDATE vault_session_leases
		SET status = 'revoked', updated_at = ?
		WHERE session_id = ? AND session_generation = ? AND status = 'active'`,
		time.Now().UTC().Format(time.RFC3339Nano), sessionID, generation,
	)
	return err
}

func (p *Persistence) RevokeAll(ctx context.Context) error {
	if p == nil || p.database == nil {
		return ErrPersistenceUnavailable
	}
	_, err := p.database.ExecContext(ctx, `
		UPDATE vault_session_leases
		SET status = 'revoked', updated_at = ?
		WHERE status = 'active'`,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (p *Persistence) ActiveForProject(ctx context.Context, projectID int64) ([]Reference, error) {
	if p == nil || p.database == nil {
		return nil, ErrPersistenceUnavailable
	}
	active, err := vaultfinalization.NewStore(p.database).ActiveProjectReferences(ctx, projectID)
	if err != nil {
		return nil, err
	}
	references := make([]Reference, len(active))
	for index, reference := range active {
		references[index] = Reference{
			SessionID: reference.SessionID, RuntimeID: reference.RuntimeID, Generation: reference.Generation,
		}
	}
	return references, nil
}

func (p *Persistence) RuntimeIDsForTargetProfile(ctx context.Context, targetID, profileID int64) ([]int64, error) {
	if p == nil || p.database == nil {
		return nil, ErrPersistenceUnavailable
	}
	query := `SELECT id FROM connector_runtime_surfaces WHERE target_id = ?`
	args := []any{targetID}
	if profileID > 0 {
		query += ` AND profile_id = ?`
		args = append(args, profileID)
	}
	query += ` ORDER BY id`
	return p.queryRuntimeIDs(ctx, query, args...)
}

func (p *Persistence) AllRuntimeIDs(ctx context.Context) ([]int64, error) {
	if p == nil || p.database == nil {
		return nil, ErrPersistenceUnavailable
	}
	return p.queryRuntimeIDs(ctx, `SELECT id FROM connector_runtime_surfaces ORDER BY id`)
}

func (p *Persistence) ActiveEnvironmentSessionsForRuntimes(ctx context.Context, runtimeIDs []int64) ([]Reference, error) {
	if p == nil || p.database == nil {
		return nil, ErrPersistenceUnavailable
	}
	references := make([]Reference, 0)
	seen := make(map[int64]struct{})
	for _, runtimeID := range runtimeIDs {
		if runtimeID < 1 {
			continue
		}
		rows, err := p.database.QueryContext(ctx, `
			SELECT id, runtime_id, generation
			FROM console_sessions
			WHERE runtime_id = ?
			  AND status IN ('connecting', 'connected')
			  AND environment_content_hash <> ''
			ORDER BY id`, runtimeID)
		if err != nil {
			return nil, fmt.Errorf("list active Vault sessions for runtime %d: %w", runtimeID, err)
		}
		items, scanErr := scanReferences(rows)
		closeErr := rows.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		for _, item := range items {
			if _, exists := seen[item.SessionID]; exists {
				continue
			}
			seen[item.SessionID] = struct{}{}
			references = append(references, item)
		}
	}
	return references, nil
}

type rowScanner interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanReferences(rows rowScanner) ([]Reference, error) {
	references := make([]Reference, 0)
	for rows.Next() {
		var reference Reference
		if err := rows.Scan(&reference.SessionID, &reference.RuntimeID, &reference.Generation); err != nil {
			return nil, err
		}
		references = append(references, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return references, nil
}

func (p *Persistence) queryRuntimeIDs(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := p.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list connector runtime surfaces for Vault invalidation: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}
