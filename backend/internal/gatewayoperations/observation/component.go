// Package observation composes workspace audit, history, and retention
// capabilities behind the gateway operations boundary.
package observation

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/db"
	historyhttp "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type Appender = observability.Appender
type ActiveRuntime func(http.ResponseWriter) (workspaceruntime.Port, bool)
type StartActions func(workspaceruntime.Port)

type Component struct {
	health observability.HealthTracker
}

func New() *Component { return &Component{} }

func (component *Component) ConfigureDispatcher(runtime workspaceruntime.Port) {
	if runtime == nil || runtime.StoragePort().DatabaseHandle() == nil || runtime.ObservationPort().AuditDispatcherService() != nil {
		return
	}
	dispatcher := observability.NewDispatcher(runtime.StoragePort().DatabaseHandle())
	runtime.ObservationPort().SetAuditDispatcherService(dispatcher)
	dispatcher.Start()
}

func (component *Component) InitializeRetention(runtime workspaceruntime.Port, startActions StartActions) {
	if runtime == nil || runtime.StoragePort().DatabaseHandle() == nil {
		return
	}
	if runtime.ObservationPort().RetentionService() == nil {
		runtime.ObservationPort().SetRetentionService(retention.NewService(runtime.StoragePort().DatabaseHandle(), runtime.DatabaseIdentifier()))
	}
	runtime.ObservationPort().RetentionService().Start()
	if startActions != nil {
		startActions(runtime)
	}
}

func (component *Component) HealthSnapshot(ctx context.Context, runtime workspaceruntime.Port) observability.HealthSnapshot {
	if runtime == nil {
		return component.health.Snapshot(ctx, nil)
	}
	return component.health.Snapshot(ctx, runtime.StoragePort().DatabaseHandle())
}

func (component *Component) RecordFailure(at time.Time) {
	component.health.RecordFailure(at)
}

func (component *Component) PrepareRedactor(ctx context.Context, runtime workspaceruntime.Port) func(string) string {
	if runtime == nil || runtime.SecurityPort().PolicyService() == nil {
		return securitypolicy.RedactBasic
	}
	return runtime.SecurityPort().PolicyService().PrepareRedactor(ctx)
}

func (component *Component) WriteObservation(
	ctx context.Context,
	runtime workspaceruntime.Port,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload any,
) {
	if err := component.WriteRequired(ctx, runtime, actorType, tokenID, runtimeID, action, payload); err != nil {
		log.Printf("audit write failed actor=%q runtime_id=%d action=%q error=%v", actorType, runtimeID, action, err)
	}
}

func (component *Component) WriteRequired(
	ctx context.Context,
	runtime workspaceruntime.Port,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload any,
) (err error) {
	defer func() {
		if err != nil {
			component.health.RecordFailure(time.Now())
		}
	}()
	return component.coordinator(ctx, runtime).WriteRequired(ctx, actorType, tokenID, runtimeID, action, payload)
}

func (component *Component) WithTransaction(
	ctx context.Context,
	runtime workspaceruntime.Port,
	mutate func(*sql.Tx, Appender) error,
) error {
	return component.coordinator(ctx, runtime).WithTransaction(ctx, mutate)
}

func (component *Component) WithMutation(
	ctx context.Context,
	runtime workspaceruntime.Port,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	return component.coordinator(ctx, runtime).WithMutation(ctx, actorType, tokenID, runtimeID, action, payload, mutate)
}

func (component *Component) Project(ctx context.Context, runtime workspaceruntime.Port) {
	if runtime != nil {
		component.newCoordinator(runtime, nil).Project(ctx)
	}
}

func (component *Component) VaultRequestStore(ctx context.Context, runtime workspaceruntime.Port) *vaultrequests.Store {
	redact := component.PrepareRedactor(ctx, runtime)
	return vaultrequests.NewStore(runtime.StoragePort().DatabaseHandle()).WithMutationHook(func(ctx context.Context, executor vaultrequests.Executor, item vaultrequests.Request) error {
		event, err := observability.BuildEvent(ctx, executor, observability.BuildInput{
			ActorType: "gateway", TokenID: pointer(item.TokenID), RuntimeID: valueOrZero(item.RuntimeID),
			Action:  "vault.action_request." + item.Status,
			Payload: vaultrequests.RequestAuditPayload(item, item.UserNote), Redact: redact,
		})
		if err != nil {
			return err
		}
		_, err = (observability.Store{}).Append(ctx, executor, event)
		return err
	})
}

func (component *Component) SyncVaultActionRequest(ctx context.Context, runtime workspaceruntime.Port, id int64) error {
	return historyhttp.NewStore(runtime.StoragePort().DatabaseHandle()).SyncVaultActionRequest(ctx, id)
}

func (component *Component) Diagnostics(ctx context.Context, runtime workspaceruntime.Port) (observability.Report, error) {
	audit := component.HealthSnapshot(ctx, runtime)
	return observability.Collect(ctx, observability.CollectInput{
		Database: runtime.StoragePort().DatabaseHandle(), Registry: runtime.ConnectorPort().ConnectorRegistry(),
		SupportedSchemaVersion: db.CurrentSchemaVersion(), MCPEnabled: runtime.IsMCPStarted(),
		Audit: observability.AuditHealth{
			Status: audit.Status, FailureCount: audit.FailureCount, PendingCount: audit.PendingCount,
			DeadLetterCount: audit.DeadLetterCount, RetriedEventCount: audit.RetriedEventCount,
		},
	})
}

func ReportFormatVersion() string { return observability.ReportFormatVersion }

func pointer(value int64) *int64 { return &value }

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (component *Component) coordinator(ctx context.Context, runtime workspaceruntime.Port) *observability.Coordinator {
	if runtime == nil || runtime.StoragePort().DatabaseHandle() == nil {
		return observability.NewCoordinator(nil, nil, nil, component.projectionFailureHandler())
	}
	return component.newCoordinator(runtime, component.PrepareRedactor(ctx, runtime))
}

func (component *Component) newCoordinator(runtime workspaceruntime.Port, redact func(string) string) *observability.Coordinator {
	return observability.NewCoordinator(runtime.StoragePort().DatabaseHandle(), runtime.ObservationPort().AuditDispatcherService(), redact, component.projectionFailureHandler())
}

func (component *Component) projectionFailureHandler() observability.ProjectionFailureHandler {
	return func(action string, err error) {
		component.health.RecordFailure(time.Now())
		if action == "" {
			log.Printf("audit projection failed error=%v", err)
			return
		}
		log.Printf("audit projection failed action=%q error=%v", action, err)
	}
}

type HTTPHandlers struct {
	Retention *retention.HTTPHandlers
	History   *historyhttp.Handlers
	Audit     *observability.HTTPHandlers
}

func (component *Component) HTTPHandlers(active ActiveRuntime) HTTPHandlers {
	return HTTPHandlers{
		Retention: retention.NewHTTPHandlers(component.retentionScope(active)),
		History:   historyhttp.New(component.historyScope(active)),
		Audit:     observability.NewHTTPHandlers(component.auditScope(active)),
	}
}

func (component *Component) auditScope(active ActiveRuntime) observability.HTTPScopeProvider {
	return func(w http.ResponseWriter) (observability.HTTPScope, bool) {
		runtime, ok := active(w)
		if !ok {
			return observability.HTTPScope{}, false
		}
		return observability.HTTPScope{Database: runtime.StoragePort().DatabaseHandle()}, true
	}
}

func (component *Component) historyScope(active ActiveRuntime) historyhttp.ScopeProvider {
	return func(w http.ResponseWriter) (historyhttp.Scope, bool) {
		runtime, ok := active(w)
		if !ok {
			return historyhttp.Scope{}, false
		}
		return historyhttp.Scope{
			Database: runtime.StoragePort().DatabaseHandle(),
			Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return component.WithMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
			},
		}, true
	}
}

func (component *Component) retentionScope(active ActiveRuntime) retention.HTTPScopeProvider {
	return func(w http.ResponseWriter) (retention.HTTPScope, bool) {
		runtime, ok := active(w)
		if !ok {
			return retention.HTTPScope{}, false
		}
		if runtime.ObservationPort().RetentionService() == nil {
			return retention.HTTPScope{}, true
		}
		return retention.HTTPScope{
			Service: runtime.ObservationPort().RetentionService(),
			Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return component.WithMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
			},
		}, true
	}
}
