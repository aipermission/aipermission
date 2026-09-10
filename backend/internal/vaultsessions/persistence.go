package vaultsessions

import (
	"context"
	"errors"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

var ErrPersistenceUnavailable = errors.New("Vault session lease persistence is unavailable")

type Persistence struct{ database sqldb.Executor }

func NewPersistence(database sqldb.Executor) *Persistence {
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
