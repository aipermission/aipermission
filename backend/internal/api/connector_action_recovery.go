package api

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	connectorActionRecoveryInterval = 15 * time.Second
	connectorActionOrphanAge        = 30 * time.Second
)

const connectorActionPersistenceUnknownMessage = "the connector action may have completed, but AIPermission could not persist its final result; inspect external state before retrying"

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
		WHERE status = ? AND julianday(created_at) <= julianday(?)
		ORDER BY id`,
		string(connectors.ResultRunning),
		now.Add(-connectorActionOrphanAge).Format(time.RFC3339Nano),
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
		_, err := s.persistConnectorActionFinish(ctx, runtime, id, connectors.ResultOutcomeUnknown, connectors.ActionResult{
			Status: connectors.ResultOutcomeUnknown,
			Error:  connectorActionPersistenceUnknownMessage,
		}, []connectors.ResultStatus{connectors.ResultRunning})
		if err != nil {
			log.Printf("recover orphaned connector action failed workspace=%s request=%d error=%v", runtime.id, id, err)
		}
	}
}
