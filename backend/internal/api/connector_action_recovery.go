package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const (
	connectorActionRecoveryInterval = 15 * time.Second
	connectorActionOrphanAge        = 30 * time.Second
)

const connectorActionPersistenceUnknownMessage = "the connector action may have completed, but AIPermission could not persist its final result; inspect external state before retrying"
const connectorActionLeaseExpiredBeforeDispatchMessage = "connector action execution lease expired before dispatch; no remote action was attempted"

func connectorActionLeaseExpiry(now time.Time) time.Time {
	return now.Add(connectorActionOrphanAge)
}

type connectorActionRecoveryWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *Server) startConnectorActionRecoveryWorker(runtime *databaseRuntime) {
	if runtime == nil || runtime.database == nil || runtime.actionRecovery.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	runtime.actionRecovery.cancel = cancel
	runtime.actionRecovery.done = done

	go func() {
		defer close(done)
		ticker := time.NewTicker(connectorActionRecoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.recoverOrphanedConnectorActions(ctx, runtime, time.Now().UTC())
			}
		}
	}()
}

func (s *Server) stopConnectorActionRecoveryWorker(runtime *databaseRuntime) {
	if runtime == nil || runtime.actionRecovery.cancel == nil {
		return
	}
	cancel := runtime.actionRecovery.cancel
	done := runtime.actionRecovery.done
	runtime.actionRecovery = connectorActionRecoveryWorker{}
	cancel()
	if done != nil {
		<-done
	}
}

func (s *Server) recoverOrphanedConnectorActions(workerContext context.Context, runtime *databaseRuntime, now time.Time) {
	ctx, cancel := context.WithTimeout(workerContext, connectorActionFinishTimeout)
	defer cancel()
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT id
		FROM connector_action_requests
		WHERE status = ?
		  AND (execution_lease_expires_at = '' OR julianday(execution_lease_expires_at) <= julianday(?))
		ORDER BY id`,
		string(connectors.ResultRunning),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("list orphaned connector actions failed workspace=%s error=%v", runtime.id, err)
		}
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			log.Printf("scan orphaned connector action failed workspace=%s error=%v", runtime.id, err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		log.Printf("iterate orphaned connector actions failed workspace=%s error=%v", runtime.id, err)
		return
	}
	if err := rows.Close(); err != nil {
		log.Printf("close orphaned connector action rows failed workspace=%s error=%v", runtime.id, err)
		return
	}
	for _, id := range ids {
		if _, active := runtime.connectorCredentialBoundary(id); active {
			continue
		}
		_, err := s.persistExpiredConnectorActionRecovery(ctx, runtime, id, now)
		if err != nil {
			log.Printf("recover orphaned connector action failed workspace=%s request=%d error=%v", runtime.id, id, err)
		}
	}
}

func (s *Server) persistExpiredConnectorActionRecovery(ctx context.Context, runtime *databaseRuntime, requestID int64, now time.Time) (connectortargets.ActionRequest, error) {
	var recovered connectortargets.ActionRequest
	err := s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		var changed bool
		var err error
		recovered, changed, err = connectortargets.NewTxStore(tx).RecoverExpiredActionRequest(
			ctx, requestID, now, connectorActionLeaseExpiredBeforeDispatchMessage, connectorActionPersistenceUnknownMessage,
		)
		if err != nil {
			return err
		}
		if !changed {
			return errAuditedMutationUnchanged
		}
		return appendAudit(
			tx, "gateway", recovered.TokenID, 0,
			"connector_action.request."+string(recovered.Status),
			connectorActionRequestAuditPayload(recovered),
		)
	})
	if errors.Is(err, errAuditedMutationUnchanged) {
		return recovered, nil
	}
	return recovered, err
}

func (s *Server) beginConnectorActionDispatch(ctx context.Context, runtime *databaseRuntime, requestID int64) (connectortargets.ActionRequest, bool, error) {
	now := time.Now().UTC()
	request, err := connectortargets.NewStore(runtime.database).BeginActionRequestDispatch(
		ctx, requestID, runtime.runtimeInstanceID, now, connectorActionLeaseExpiry(now),
	)
	if err == nil {
		return request, true, nil
	}
	if !errors.Is(err, connectortargets.ErrActionRequestExecutionClaim) {
		return connectortargets.ActionRequest{}, false, err
	}
	current, getErr := connectortargets.NewStore(runtime.database).GetActionRequest(ctx, requestID)
	if getErr != nil {
		return connectortargets.ActionRequest{}, false, getErr
	}
	if current.Status != connectors.ResultRunning {
		return current, false, nil
	}
	leaseExpiresAt, leaseErr := time.Parse(time.RFC3339Nano, current.ExecutionLeaseExpiresAt)
	if current.ExecutionOwner != runtime.runtimeInstanceID || current.DispatchStartedAt != "" || leaseErr != nil || leaseExpiresAt.After(now) {
		return current, false, connectortargets.ErrActionRequestExecutionClaim
	}
	finished, finishErr := s.finishConnectorActionRequest(
		context.Background(), runtime, requestID, connectors.ResultFailed, nil, "",
		connectorActionLeaseExpiredBeforeDispatchMessage, connectors.OutputHint{},
	)
	if finishErr != nil {
		latest, latestErr := connectortargets.NewStore(runtime.database).GetActionRequest(ctx, requestID)
		if latestErr == nil && latest.Status != connectors.ResultRunning {
			return latest, false, nil
		}
		return connectortargets.ActionRequest{}, false, finishErr
	}
	return finished, false, nil
}
