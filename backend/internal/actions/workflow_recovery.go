package actions

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/timeformat"
)

const actionRecoveryInterval = 15 * time.Second

const PersistenceUnknownMessage = "the connector action may have completed, but AIPermission could not persist its final result; inspect external state before retrying"
const LeaseExpiredBeforeDispatchMessage = "connector action execution lease expired before dispatch; no remote action was attempted"

func (r *Runtime) StartRecovery() {
	if r == nil || r.validate() != nil {
		return
	}
	r.recoveryMu.Lock()
	if r.recoveryCancel != nil {
		r.recoveryMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.recoveryCancel, r.recoveryDone = cancel, done
	r.recoveryMu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(actionRecoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.Recover(ctx, r.now().UTC())
			}
		}
	}()
}

func (r *Runtime) StopRecovery() {
	if r == nil {
		return
	}
	r.recoveryMu.Lock()
	cancel, done := r.recoveryCancel, r.recoveryDone
	r.recoveryCancel, r.recoveryDone = nil, nil
	r.recoveryMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
}

func (r *Runtime) Recover(workerContext context.Context, now time.Time) {
	if err := r.validate(); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(workerContext, actionFinishTimeout)
	defer cancel()
	rows, err := r.database.QueryContext(ctx, `
		SELECT id
		FROM connector_action_requests
		WHERE status = ?
		  AND (execution_lease_expires_at = '' OR julianday(execution_lease_expires_at) <= julianday(?))
		ORDER BY id`, string(connectors.ResultRunning), timeformat.UTC(now))
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			r.logf("list orphaned connector actions failed: %v", err)
		}
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			r.logf("scan orphaned connector action failed: %v", err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		r.logf("iterate orphaned connector actions failed: %v", err)
		return
	}
	if err := rows.Close(); err != nil {
		r.logf("close orphaned connector action rows failed: %v", err)
		return
	}
	for _, id := range ids {
		if _, active := r.CredentialBoundary(id); active {
			continue
		}
		if _, err := r.PersistExpiredRecovery(ctx, id, now); err != nil {
			r.logf("recover orphaned connector action failed request=%d error=%v", id, err)
		}
	}
}

func (r *Runtime) PersistExpiredRecovery(ctx context.Context, requestID int64, now time.Time) (connectortargets.ActionRequest, error) {
	var recovered connectortargets.ActionRequest
	err := r.mutations.WithTransaction(ctx, func(tx *sql.Tx, appendAudit AuditAppender) error {
		var changed bool
		var err error
		recovered, changed, err = connectortargets.NewTxStore(tx).RecoverExpiredActionRequest(ctx, requestID, now, LeaseExpiredBeforeDispatchMessage, PersistenceUnknownMessage)
		if err != nil {
			return err
		}
		if !changed {
			return ErrMutationUnchanged
		}
		return appendAudit(tx, "gateway", recovered.TokenID, 0, "connector_action.request."+string(recovered.Status), RequestAuditPayload(recovered))
	})
	if errors.Is(err, ErrMutationUnchanged) {
		return recovered, nil
	}
	return recovered, err
}

func (r *Runtime) BeginDispatch(ctx context.Context, requestID int64) (connectortargets.ActionRequest, bool, error) {
	_, runtimeInstanceID, err := r.runtimeIdentity()
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	now := r.now().UTC()
	store := connectortargets.NewStore(r.database)
	request, err := store.BeginActionRequestDispatch(ctx, requestID, runtimeInstanceID, now, LeaseExpiry(now))
	if err == nil {
		return request, true, nil
	}
	if !errors.Is(err, connectortargets.ErrActionRequestExecutionClaim) {
		return connectortargets.ActionRequest{}, false, err
	}
	current, getErr := store.GetActionRequest(ctx, requestID)
	if getErr != nil {
		return connectortargets.ActionRequest{}, false, getErr
	}
	if current.Status != connectors.ResultRunning {
		return current, false, nil
	}
	leaseExpiresAt, leaseErr := time.Parse(time.RFC3339Nano, current.ExecutionLeaseExpiresAt)
	if current.ExecutionOwner != runtimeInstanceID || current.DispatchStartedAt != "" || leaseErr != nil || leaseExpiresAt.After(now) {
		return current, false, connectortargets.ErrActionRequestExecutionClaim
	}
	finished, finishErr := r.Finish(context.Background(), requestID, connectors.ResultFailed, nil, "", LeaseExpiredBeforeDispatchMessage, connectors.OutputHint{})
	if finishErr != nil {
		latest, latestErr := store.GetActionRequest(ctx, requestID)
		if latestErr == nil && latest.Status != connectors.ResultRunning {
			return latest, false, nil
		}
		return connectortargets.ActionRequest{}, false, finishErr
	}
	return finished, false, nil
}

func (r *Runtime) MarkRunningOutcomeUnknown(ctx context.Context, message string) error {
	if err := r.validate(); err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	return r.mutations.WithTransaction(ctx, func(tx *sql.Tx, appendAudit AuditAppender) error {
		requests, err := connectortargets.NewTxStore(tx).MarkRunningOutcomeUnknown(ctx, message, r.now().UTC())
		if err != nil {
			return err
		}
		for _, request := range requests {
			if err := appendAudit(tx, "gateway", request.TokenID, 0, "connector_action.request."+string(connectors.ResultOutcomeUnknown), map[string]any{
				"request_id": request.ID, "project_id": request.ProjectID,
				"target_id": request.TargetID, "profile_id": request.ProfileID,
				"connector_kind": request.ConnectorKind, "action_name": request.ActionName,
				"status": connectors.ResultOutcomeUnknown, "error": message,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
