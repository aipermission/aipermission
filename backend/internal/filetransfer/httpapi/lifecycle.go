package filetransferhttp

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

// Lifecycle owns the worker registry and outcome-persistence lifetime for one
// unlocked workspace. API composition does not need their concrete types.
type Lifecycle struct {
	jobs         transferjobs.Registry
	finalization transferjobs.FinalizationLifetime
}

func NewLifecycle() *Lifecycle {
	return &Lifecycle{finalization: transferjobs.NewFinalizationLifetime()}
}

func (l *Lifecycle) runtimeDependencies(
	database *sql.DB,
	observe ObservationAudit,
	connectorPorts ConnectorPortsResolver,
) RuntimeDependencies {
	if l == nil {
		return RuntimeDependencies{}
	}
	return RuntimeDependencies{
		Database: database, Jobs: &l.jobs, Finalization: l.finalization,
		Observe: observe, ConnectorPorts: connectorPorts,
	}
}

func (l *Lifecycle) NewRuntime(
	database *sql.DB,
	observe ObservationAudit,
	connectorPorts ConnectorPortsResolver,
) (*Runtime, error) {
	return NewRuntime(l.runtimeDependencies(database, observe, connectorPorts))
}

func (l *Lifecycle) Stop() {
	if l != nil {
		l.finalization.Stop()
	}
}

// Registry exposes worker controls to tests and local runtime wiring while the
// workspace lifecycle remains the sole owner of their shutdown.
func (l *Lifecycle) Registry() *transferjobs.Registry {
	if l == nil {
		return nil
	}
	return &l.jobs
}

func (l *Lifecycle) Wait(ctx context.Context) bool {
	return l == nil || l.jobs.Wait(ctx)
}
