package transferruntime

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

// Lifecycle owns transfer workers and terminal-state persistence for one
// unlocked workspace.
type Lifecycle struct {
	jobs         transferjobs.Registry
	finalization transferjobs.FinalizationLifetime
}

func NewLifecycle() *Lifecycle {
	return &Lifecycle{finalization: transferjobs.NewFinalizationLifetime()}
}

func (l *Lifecycle) NewRuntime(
	storageID string,
	database *sql.DB,
	observe ObservationAudit,
	connectorPorts ConnectorPortsResolver,
) (*Runtime, error) {
	if l == nil {
		return NewRuntime(RuntimeDependencies{})
	}
	return NewRuntime(RuntimeDependencies{
		StorageID: storageID, Database: database, Jobs: &l.jobs, Finalization: l.finalization,
		Observe: observe, ConnectorPorts: connectorPorts,
	})
}

func (l *Lifecycle) Stop() {
	if l != nil {
		l.finalization.Stop()
	}
}

func (l *Lifecycle) Registry() *transferjobs.Registry {
	if l == nil {
		return nil
	}
	return &l.jobs
}

func (l *Lifecycle) Wait(ctx context.Context) bool {
	return l == nil || l.jobs.Wait(ctx)
}

func (l *Lifecycle) Abort(ctx context.Context) bool {
	if l == nil {
		return true
	}
	l.Stop()
	l.jobs.BeginShutdown()
	return l.jobs.Wait(ctx)
}
