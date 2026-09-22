package accesscontrol

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func mutateAuthorization(
	ctx context.Context,
	scope Scope,
	tokenID int64,
	event string,
	payload func() any,
	invalidationReason string,
	mutate func(*sql.Tx) (bool, error),
) (bool, error) {
	release, err := scope.AcquireExclusive(ctx)
	if err != nil {
		return false, ErrVaultDeliveryCanceled
	}
	defer release()
	changed := false
	sessionIDs := []int64{}
	err = scope.Mutate(ctx, event, payload, func(tx *sql.Tx) error {
		var mutationErr error
		changed, mutationErr = mutate(tx)
		if mutationErr != nil {
			return mutationErr
		}
		if !changed {
			return ErrAuthorizationUnchanged
		}
		rows, queryErr := tx.QueryContext(ctx, `
			SELECT DISTINCT session_id
			FROM vault_session_leases
			WHERE token_id = ? AND status = 'active'
			ORDER BY session_id`, tokenID)
		if queryErr != nil {
			return queryErr
		}
		for rows.Next() {
			var sessionID int64
			if scanErr := rows.Scan(&sessionID); scanErr != nil {
				_ = rows.Close()
				return scanErr
			}
			sessionIDs = append(sessionIDs, sessionID)
		}
		if iterationErr := rows.Err(); iterationErr != nil {
			_ = rows.Close()
			return iterationErr
		}
		if closeErr := rows.Close(); closeErr != nil {
			return closeErr
		}
		if _, updateErr := tx.ExecContext(ctx, `
			UPDATE vault_session_leases
			SET status = 'revoked', updated_at = ?
			WHERE token_id = ? AND status = 'active'`,
			time.Now().UTC().Format(time.RFC3339Nano), tokenID,
		); updateErr != nil {
			return updateErr
		}
		if err := vaultrequests.NewTxStore(tx).StalePendingForToken(ctx, tokenID, invalidationReason); err != nil {
			return err
		}
		_, err := connectortargets.NewTxStore(tx).StalePendingActionRequestsForToken(ctx, tokenID, invalidationReason)
		return err
	})
	if errors.Is(err, ErrAuthorizationUnchanged) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// The durable authorization, lease, request, and audit state is already
	// committed. Live cleanup must not turn that success into an ambiguous
	// client error; persisted revocation remains authoritative and the adapter
	// records cleanup failures for subsequent runtime recovery.
	scope.FinishTokenInvalidation(ctx, tokenID, sessionIDs)
	return true, nil
}
