// Package observation composes workspace audit, history, and retention
// capabilities behind the gateway operations boundary.
package observation

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/db"
	historyhttp "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type Appender func(*sql.Tx, string, *int64, int64, string, any) error
type ActiveRuntime func(http.ResponseWriter) (Runtime, bool)
type StartActions func()

// Runtime is the observation-owned view of one unlocked workspace. Bound
// callbacks keep mutable workspace state behind the composition boundary.
type Runtime struct {
	Database            *sql.DB
	DatabaseID          string
	Registry            *connectors.Registry
	MCPStarted          bool
	PrepareRedactor     func(context.Context) func(string) string
	AuditDispatcher     func() *observability.Dispatcher
	SetAuditDispatcher  func(*observability.Dispatcher)
	RetentionService    func() *retention.Service
	SetRetentionService func(*retention.Service)
}

type Component struct {
	health observability.HealthTracker
}

func New() *Component { return &Component{} }

func (component *Component) ConfigureDispatcher(runtime Runtime) {
	if runtime.Database == nil || runtime.AuditDispatcher == nil || runtime.SetAuditDispatcher == nil || runtime.AuditDispatcher() != nil {
		return
	}
	dispatcher := observability.NewDispatcher(runtime.Database)
	runtime.SetAuditDispatcher(dispatcher)
	dispatcher.Start()
}

func (component *Component) InitializeRetention(runtime Runtime, startActions StartActions) {
	if runtime.Database == nil || runtime.RetentionService == nil || runtime.SetRetentionService == nil {
		return
	}
	if runtime.RetentionService() == nil {
		runtime.SetRetentionService(retention.NewService(runtime.Database, runtime.DatabaseID))
	}
	runtime.RetentionService().Start()
	if startActions != nil {
		startActions()
	}
}

func (component *Component) HealthSnapshot(ctx context.Context, runtime Runtime) observability.HealthSnapshot {
	if runtime.Database == nil {
		return component.health.Snapshot(ctx, nil)
	}
	return component.health.Snapshot(ctx, runtime.Database)
}

func (component *Component) RecordFailure(at time.Time) {
	component.health.RecordFailure(at)
}

func (component *Component) PrepareRedactor(ctx context.Context, runtime Runtime) func(string) string {
	if runtime.PrepareRedactor == nil {
		return securitypolicy.RedactBasic
	}
	redact := runtime.PrepareRedactor(ctx)
	if redact == nil {
		return securitypolicy.RedactBasic
	}
	return redact
}

func (component *Component) WriteObservation(
	ctx context.Context,
	runtime Runtime,
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
	runtime Runtime,
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
	runtime Runtime,
	mutate func(*sql.Tx, Appender) error,
) error {
	return component.coordinator(ctx, runtime).WithTransaction(ctx, func(tx *sql.Tx, appendObservation observability.Appender) error {
		return mutate(tx, Appender(appendObservation))
	})
}

func (component *Component) WithMutation(
	ctx context.Context,
	runtime Runtime,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	return component.coordinator(ctx, runtime).WithMutation(ctx, actorType, tokenID, runtimeID, action, payload, mutate)
}

func (component *Component) Project(ctx context.Context, runtime Runtime) {
	if runtime.Database != nil {
		component.newCoordinator(runtime, nil).Project(ctx)
	}
}

func (component *Component) VaultRequestStore(ctx context.Context, runtime Runtime) *vaultrequests.Store {
	redact := component.PrepareRedactor(ctx, runtime)
	return vaultrequests.NewStore(runtime.Database).WithMutationHook(func(ctx context.Context, executor vaultrequests.Executor, item vaultrequests.Request) error {
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

func (component *Component) VaultRequestStoreFactory(runtime Runtime) func(context.Context) vaultrequests.RequestStore {
	return func(ctx context.Context) vaultrequests.RequestStore {
		return component.VaultRequestStore(ctx, runtime)
	}
}

func (component *Component) SyncVaultActionRequest(ctx context.Context, runtime Runtime, id int64) error {
	return historyhttp.NewStore(runtime.Database).SyncVaultActionRequest(ctx, id)
}

func (component *Component) Diagnostics(ctx context.Context, runtime Runtime) (observability.Report, error) {
	audit := component.HealthSnapshot(ctx, runtime)
	return observability.Collect(ctx, observability.CollectInput{
		Database: runtime.Database, Registry: runtime.Registry,
		SupportedSchemaVersion: db.CurrentSchemaVersion(), MCPEnabled: runtime.MCPStarted,
		Audit: observability.AuditHealth{
			Status: audit.Status, FailureCount: audit.FailureCount, PendingCount: audit.PendingCount,
			DeadLetterCount: audit.DeadLetterCount, RetriedEventCount: audit.RetriedEventCount,
		},
	})
}

func ReportFormatVersion() string { return observability.ReportFormatVersion }

func (component *Component) PrepareDiagnosticsDownload(w http.ResponseWriter) string {
	httpattachment.SetHeaders(
		w,
		"aipermission-diagnostics-"+time.Now().UTC().Format("20060102T150405Z")+".json",
		"application/json",
	)
	return observability.ReportFormatVersion
}

func pointer(value int64) *int64 { return &value }

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (component *Component) coordinator(ctx context.Context, runtime Runtime) *observability.Coordinator {
	if runtime.Database == nil {
		return observability.NewCoordinator(nil, nil, nil, component.projectionFailureHandler())
	}
	return component.newCoordinator(runtime, component.PrepareRedactor(ctx, runtime))
}

func (component *Component) newCoordinator(runtime Runtime, redact func(string) string) *observability.Coordinator {
	var dispatcher *observability.Dispatcher
	if runtime.AuditDispatcher != nil {
		dispatcher = runtime.AuditDispatcher()
	}
	return observability.NewCoordinator(runtime.Database, dispatcher, redact, component.projectionFailureHandler())
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
		return observability.HTTPScope{Database: runtime.Database}, true
	}
}

func (component *Component) historyScope(active ActiveRuntime) historyhttp.ScopeProvider {
	return func(w http.ResponseWriter) (historyhttp.Scope, bool) {
		runtime, ok := active(w)
		if !ok {
			return historyhttp.Scope{}, false
		}
		return historyhttp.Scope{
			Database: runtime.Database,
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
		if runtime.RetentionService == nil || runtime.RetentionService() == nil {
			return retention.HTTPScope{}, true
		}
		return retention.HTTPScope{
			Service: runtime.RetentionService(),
			Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return component.WithMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
			},
		}, true
	}
}
