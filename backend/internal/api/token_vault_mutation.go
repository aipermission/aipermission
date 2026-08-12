package api

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

var errVaultDeliveryCanceled = errors.New("Vault delivery was canceled")

func (s tokenHandlers) mutateTokenWithVaultInvalidation(
	ctx context.Context,
	runtime *databaseRuntime,
	tokenID int64,
	event string,
	payload func() any,
	invalidationReason string,
	mutate func(*sql.Tx) (bool, error),
) (bool, error) {
	release, err := runtime.vaultDelivery.acquire(ctx)
	if err != nil {
		return false, errVaultDeliveryCanceled
	}
	defer release()
	changed := false
	sessionIDs := []int64{}
	err = s.withAuditedMutation(ctx, runtime, "user", nil, 0, event, payload, func(tx *sql.Tx) error {
		var mutationErr error
		changed, mutationErr = mutate(tx)
		if mutationErr != nil {
			return mutationErr
		}
		if !changed {
			return errAuditedMutationUnchanged
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
				rows.Close()
				return scanErr
			}
			sessionIDs = append(sessionIDs, sessionID)
		}
		if iterationErr := rows.Err(); iterationErr != nil {
			rows.Close()
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
		return vaultrequests.NewTxStore(tx).StalePendingForToken(ctx, tokenID, invalidationReason)
	})
	if errors.Is(err, errAuditedMutationUnchanged) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := finishVaultTokenSessionInvalidation(ctx, runtime, tokenID, sessionIDs); err != nil {
		return true, err
	}
	return true, nil
}
