package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

const vaultSessionInvalidationTimeout = 15 * time.Second

func (s *Server) invalidateVaultMutationAfterCommit(
	ctx context.Context,
	runtime *databaseRuntime,
	sessions []projectvault.SessionReference,
	scope projectvault.SessionMutationScope,
) error {
	closeErr := closeVaultSessionReferences(ctx, runtime, sessions)
	staleErr := s.vaultRequestStore(ctx, runtime).StalePendingForContext(
		ctx, scope.ItemID, scope.BindingID, "Vault item or binding changed; send a fresh request",
	)
	return errors.Join(closeErr, staleErr)
}

func closeVaultSessionReferences(ctx context.Context, runtime *databaseRuntime, sessions []projectvault.SessionReference) error {
	var closeErrors []error
	for _, session := range sessions {
		runtime.vaultLeases.RevokeSession(console.SessionHandle{
			ID: session.SessionID, RuntimeID: session.RuntimeID, Generation: session.Generation,
		})
		if err := revokePersistedVaultLease(ctx, runtime, session.SessionID, session.Generation); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("revoke persisted Vault lease for session %d: %w", session.SessionID, err))
		}
	}
	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		closeErrors = append(closeErrors, err)
		return errors.Join(closeErrors...)
	}
	for _, session := range sessions {
		if err := runtime.consoleSessions.Close(ctx, principal, session.SessionID); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close stale Vault session %d: %w", session.SessionID, err))
		}
	}
	return errors.Join(closeErrors...)
}

func (s *Server) invalidateVaultTokenSessions(ctx context.Context, runtime *databaseRuntime, tokenID int64, reason string) error {
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT DISTINCT session_id
		FROM vault_session_leases
		WHERE token_id = ? AND status = 'active'`, tokenID)
	if err != nil {
		return err
	}
	sessionIDs := []int64{}
	for rows.Next() {
		var sessionID int64
		if err := rows.Scan(&sessionID); err != nil {
			rows.Close()
			return err
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var invalidationErrors []error
	if _, err := runtime.database.ExecContext(ctx, `
		UPDATE vault_session_leases
		SET status = 'revoked', updated_at = ?
		WHERE token_id = ? AND status = 'active'`,
		now, tokenID,
	); err != nil {
		invalidationErrors = append(invalidationErrors, err)
	}
	if err := s.vaultRequestStore(ctx, runtime).StalePendingForToken(ctx, tokenID, reason); err != nil {
		invalidationErrors = append(invalidationErrors, err)
	}
	if err := finishVaultTokenSessionInvalidation(ctx, runtime, tokenID, sessionIDs); err != nil {
		invalidationErrors = append(invalidationErrors, err)
	}
	return errors.Join(invalidationErrors...)
}

func finishVaultTokenSessionInvalidation(ctx context.Context, runtime *databaseRuntime, tokenID int64, sessionIDs []int64) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), vaultSessionInvalidationTimeout)
	defer cancel()
	runtime.vaultLeases.RevokeToken(tokenID)
	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		return err
	}
	var closeErrors []error
	for _, sessionID := range sessionIDs {
		if err := runtime.consoleSessions.Close(cleanupCtx, principal, sessionID); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close token Vault session %d: %w", sessionID, err))
		}
	}
	return errors.Join(closeErrors...)
}

func (s *Server) invalidateVaultProjectSessions(ctx context.Context, runtime *databaseRuntime, projectID int64, reason string) error {
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT DISTINCT session_id, runtime_id, session_generation
		FROM vault_session_leases
		WHERE project_id = ? AND status = 'active'
		ORDER BY session_id`,
		projectID,
	)
	if err != nil {
		return err
	}
	references := []projectvault.SessionReference{}
	for rows.Next() {
		var reference projectvault.SessionReference
		if err := rows.Scan(&reference.SessionID, &reference.RuntimeID, &reference.Generation); err != nil {
			rows.Close()
			return err
		}
		references = append(references, reference)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	closeErr := closeVaultSessionReferences(ctx, runtime, references)
	staleErr := s.vaultRequestStore(ctx, runtime).StalePendingForProject(ctx, projectID, reason)
	return errors.Join(closeErr, staleErr)
}

func revokeAllPersistedVaultLeases(ctx context.Context, runtime *databaseRuntime) error {
	if runtime == nil || runtime.database == nil {
		return errors.New("Vault runtime is unavailable")
	}
	_, err := runtime.database.ExecContext(ctx, `
		UPDATE vault_session_leases
		SET status = 'revoked', updated_at = ?
		WHERE status = 'active'`,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func queryVaultRuntimeIDs(ctx context.Context, runtime *databaseRuntime, where string, args ...any) ([]int64, error) {
	query := "SELECT id FROM connector_runtime_surfaces"
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY id"
	rows, err := runtime.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func vaultRuntimeIDsForTargetProfile(ctx context.Context, runtime *databaseRuntime, targetID, profileID int64) ([]int64, error) {
	return queryVaultRuntimeIDs(ctx, runtime, "target_id = ? AND (? = 0 OR profile_id = ?)",
		targetID, profileID, profileID,
	)
}

func vaultAllRuntimeIDs(ctx context.Context, runtime *databaseRuntime) ([]int64, error) {
	return queryVaultRuntimeIDs(ctx, runtime, "")
}

func (s *Server) invalidateVaultRuntimeSessions(ctx context.Context, runtime *databaseRuntime, runtimeIDs []int64, reason string) error {
	if len(runtimeIDs) == 0 {
		return nil
	}
	references := []projectvault.SessionReference{}
	seenSessions := map[int64]bool{}
	for _, runtimeID := range runtimeIDs {
		if runtimeID < 1 {
			continue
		}
		rows, err := runtime.database.QueryContext(ctx, `
			SELECT id, runtime_id, generation
			FROM console_sessions
			WHERE runtime_id = ?
			  AND status IN ('connecting', 'connected')
			  AND environment_content_hash <> ''
			ORDER BY id`,
			runtimeID,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item projectvault.SessionReference
			if err := rows.Scan(&item.SessionID, &item.RuntimeID, &item.Generation); err != nil {
				rows.Close()
				return err
			}
			if !seenSessions[item.SessionID] {
				seenSessions[item.SessionID] = true
				references = append(references, item)
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	closeErr := closeVaultSessionReferences(ctx, runtime, references)
	staleErr := s.vaultRequestStore(ctx, runtime).StalePendingForRuntimes(ctx, runtimeIDs, reason)
	return errors.Join(closeErr, staleErr)
}

func (s *Server) invalidateVaultSessionsForTargetProfile(
	ctx context.Context,
	runtime *databaseRuntime,
	targetID int64,
	profileID int64,
	reason string,
) error {
	runtimeIDs, err := vaultRuntimeIDsForTargetProfile(ctx, runtime, targetID, profileID)
	if err != nil {
		return err
	}
	return s.invalidateVaultRuntimeSessions(ctx, runtime, runtimeIDs, reason)
}
