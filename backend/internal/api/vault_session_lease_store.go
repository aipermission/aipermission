package api

import (
	"context"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func activeConsoleRecord(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (console.Record, error) {
	return runtime.consoleSessions.ActiveRecord(ctx, runtimeID)
}

func persistVaultLease(ctx context.Context, runtime *databaseRuntime, projectID int64, lease vaultsessions.Lease) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := runtime.database.ExecContext(ctx, `
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

func revokePersistedVaultLease(ctx context.Context, runtime *databaseRuntime, sessionID, generation int64) error {
	_, err := runtime.database.ExecContext(ctx, `
		UPDATE vault_session_leases
		SET status = 'revoked', updated_at = ?
		WHERE session_id = ? AND session_generation = ? AND status = 'active'`,
		time.Now().UTC().Format(time.RFC3339Nano), sessionID, generation,
	)
	return err
}
