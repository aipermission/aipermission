package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	observationapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/observation"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type ObservationAppender func(*sql.Tx, string, *int64, int64, string, any) error

func (component *Component) observationWorkspace(handle *WorkspaceHandle) (observationapp.Runtime, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return observationapp.Runtime{}, false
	}
	return observationapp.Runtime{
		Database:   owner.Storage.DatabaseHandle(),
		DatabaseID: owner.Identity.DatabaseID,
		Registry:   owner.Connectors.ConnectorRegistry(),
		MCPStarted: owner.Security.RuntimeControlState().MCPStarted(),
		PrepareRedactor: func(ctx context.Context) func(string) string {
			policy := owner.Security.PolicyService()
			if policy == nil {
				return nil
			}
			return policy.PrepareRedactor(ctx)
		},
		AuditDispatcher:     owner.Observation.AuditDispatcherService,
		SetAuditDispatcher:  owner.Observation.SetAuditDispatcherService,
		RetentionService:    owner.Observation.RetentionService,
		SetRetentionService: owner.Observation.SetRetentionService,
	}, true
}

func (component *Component) ConfigureObservationDispatcher(handle *WorkspaceHandle) {
	if runtime, ok := component.observationWorkspace(handle); ok {
		component.observation.ConfigureDispatcher(runtime)
	}
}

func (component *Component) InitializeObservationRetention(handle *WorkspaceHandle, startActions func()) {
	if runtime, ok := component.observationWorkspace(handle); ok {
		component.observation.InitializeRetention(runtime, startActions)
	}
}

type ObservationHealth struct {
	Status              string `json:"status"`
	FailureCount        uint64 `json:"failure_count"`
	LastFailureAt       string `json:"last_failure_at,omitempty"`
	PendingCount        int64  `json:"pending_count"`
	DeadLetterCount     int64  `json:"dead_letter_count"`
	OldestPendingAt     string `json:"oldest_pending_at,omitempty"`
	RetriedEventCount   int64  `json:"retried_event_count"`
	LastDeliveryError   string `json:"last_delivery_error,omitempty"`
	LastDeliveryErrorAt string `json:"last_delivery_error_at,omitempty"`
	LastDeliverySuccess string `json:"last_delivery_success_at,omitempty"`
}

func (component *Component) ObservationHealthSnapshot(ctx context.Context, handle *WorkspaceHandle) ObservationHealth {
	runtime, _ := component.observationWorkspace(handle)
	snapshot := component.observation.HealthSnapshot(ctx, runtime)
	return ObservationHealth{
		Status: snapshot.Status, FailureCount: snapshot.FailureCount, LastFailureAt: snapshot.LastFailureAt,
		PendingCount: snapshot.PendingCount, DeadLetterCount: snapshot.DeadLetterCount,
		OldestPendingAt: snapshot.OldestPendingAt, RetriedEventCount: snapshot.RetriedEventCount,
		LastDeliveryError: snapshot.LastDeliveryError, LastDeliveryErrorAt: snapshot.LastDeliveryErrorAt,
		LastDeliverySuccess: snapshot.LastDeliverySuccess,
	}
}

func (component *Component) RecordObservationFailure(at time.Time) {
	component.observation.RecordFailure(at)
}

func (component *Component) WriteObservation(ctx context.Context, handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	runtime, _ := component.observationWorkspace(handle)
	component.observation.WriteObservation(ctx, runtime, actor, tokenID, runtimeID, action, payload)
}

func (component *Component) WriteObservationRequired(ctx context.Context, handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload any) error {
	runtime, _ := component.observationWorkspace(handle)
	return component.observation.WriteRequired(ctx, runtime, actor, tokenID, runtimeID, action, payload)
}

func (component *Component) PrepareObservationRedactor(ctx context.Context, handle *WorkspaceHandle) func(string) string {
	runtime, _ := component.observationWorkspace(handle)
	return component.observation.PrepareRedactor(ctx, runtime)
}

func (component *Component) WithObservationTransaction(ctx context.Context, handle *WorkspaceHandle, mutate func(*sql.Tx, ObservationAppender) error) error {
	runtime, _ := component.observationWorkspace(handle)
	return component.observation.WithTransaction(ctx, runtime, func(tx *sql.Tx, appendObservation observationapp.Appender) error {
		return mutate(tx, ObservationAppender(appendObservation))
	})
}

func (component *Component) WithObservationMutation(ctx context.Context, handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	runtime, _ := component.observationWorkspace(handle)
	return component.observation.WithMutation(ctx, runtime, actor, tokenID, runtimeID, action, payload, mutate)
}

func (component *Component) ProjectObservations(ctx context.Context, handle *WorkspaceHandle) {
	if runtime, ok := component.observationWorkspace(handle); ok {
		component.observation.Project(ctx, runtime)
	}
}

type ObservationHTTPHandlers struct {
	Retention Retention
	History   History
	Audit     Audit
}

func (component *Component) ObservationHTTPHandlers(active func(http.ResponseWriter) (*WorkspaceHandle, bool)) ObservationHTTPHandlers {
	handlers := component.observation.HTTPHandlers(func(w http.ResponseWriter) (observationapp.Runtime, bool) {
		handle, ok := active(w)
		if !ok {
			return observationapp.Runtime{}, false
		}
		return component.observationWorkspace(handle)
	})
	return ObservationHTTPHandlers{Retention: handlers.Retention, History: handlers.History, Audit: handlers.Audit}
}

func (component *Component) ObservationDiagnostics(ctx context.Context, handle *WorkspaceHandle) (json.RawMessage, error) {
	runtime, _ := component.observationWorkspace(handle)
	report, err := component.observation.Diagnostics(ctx, runtime)
	if err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

func (component *Component) PrepareDiagnosticsDownload(w http.ResponseWriter) string {
	return component.observation.PrepareDiagnosticsDownload(w)
}

func (component *Component) VaultRequestStoreFactory(handle *WorkspaceHandle) gatewayvault.RequestStoreFactory {
	runtime, _ := component.observationWorkspace(handle)
	return gatewayvault.RequestStoreFactory(component.observation.VaultRequestStoreFactory(runtime))
}

func (component *Component) SyncVaultActionRequest(ctx context.Context, handle *WorkspaceHandle, id int64) error {
	runtime, _ := component.observationWorkspace(handle)
	return component.observation.SyncVaultActionRequest(ctx, runtime, id)
}
