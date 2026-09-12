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

type observationAppender func(*sql.Tx, string, *int64, int64, string, any) error

func (component *ObservationOwner) observationWorkspace(handle *WorkspaceHandle) (observationapp.Runtime, bool) {
	if component == nil || component.application == nil {
		return observationapp.Runtime{}, false
	}
	return component.observationRuntime(handle)
}

func (component *ObservationOwner) observationRuntime(handle *WorkspaceHandle) (observationapp.Runtime, bool) {
	if component == nil {
		return observationapp.Runtime{}, false
	}
	capabilities, available := component.projection(handle)
	if !available {
		return observationapp.Runtime{}, false
	}
	projected := capabilities.Runtime
	capability, ok := projected.Current()
	if !ok {
		return observationapp.Runtime{}, false
	}
	return observationapp.Runtime{
		Database: capability.Database, DatabaseID: handle.Identity().DatabaseID,
		Registry: capability.Registry, MCPStarted: capability.MCPStarted != nil && capability.MCPStarted(),
		PrepareRedactor:     capability.PrepareRedactor,
		AuditDispatcher:     capability.AuditDispatcher,
		SetAuditDispatcher:  capability.SetAuditDispatcher,
		RetentionService:    capability.RetentionService,
		SetRetentionService: capability.SetRetentionService,
	}, true
}

func (component *ObservationOwner) withObservationTransaction(ctx context.Context, handle *WorkspaceHandle, mutate func(*sql.Tx, observationAppender) error) error {
	runtime, _ := component.observationRuntime(handle)
	return component.application.WithTransaction(ctx, runtime, func(tx *sql.Tx, appendObservation observationapp.Appender) error {
		return mutate(tx, observationAppender(appendObservation))
	})
}

func (component *ObservationOwner) withObservationMutation(ctx context.Context, handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	runtime, _ := component.observationRuntime(handle)
	return component.application.WithMutation(ctx, runtime, actor, tokenID, runtimeID, action, payload, mutate)
}

func (component *ObservationOwner) writeObservation(ctx context.Context, handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	runtime, _ := component.observationRuntime(handle)
	component.application.WriteObservation(ctx, runtime, actor, tokenID, runtimeID, action, payload)
}

func (component *ObservationOwner) writeObservationRequired(ctx context.Context, handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload any) error {
	runtime, _ := component.observationRuntime(handle)
	return component.application.WriteRequired(ctx, runtime, actor, tokenID, runtimeID, action, payload)
}

func (component *ObservationOwner) observationMutationRunner(handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64) observationMutationRunner {
	if _, ok := component.observationRuntime(handle); !ok {
		return nil
	}
	return func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
		return component.withObservationMutation(ctx, handle, actor, tokenID, runtimeID, action, payload, mutate)
	}
}

func (component *ObservationOwner) ConfigureObservationDispatcher(handle *WorkspaceHandle) {
	if runtime, ok := component.observationWorkspace(handle); ok {
		component.application.ConfigureDispatcher(runtime)
	}
}

func (component *ObservationOwner) InitializeObservationRetention(handle *WorkspaceHandle, startActions func()) {
	if runtime, ok := component.observationWorkspace(handle); ok {
		component.application.InitializeRetention(runtime, startActions)
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

func (component *ObservationOwner) ObservationHealthSnapshot(ctx context.Context, handle *WorkspaceHandle) ObservationHealth {
	runtime, _ := component.observationWorkspace(handle)
	snapshot := component.application.HealthSnapshot(ctx, runtime)
	return ObservationHealth{
		Status: snapshot.Status, FailureCount: snapshot.FailureCount, LastFailureAt: snapshot.LastFailureAt,
		PendingCount: snapshot.PendingCount, DeadLetterCount: snapshot.DeadLetterCount,
		OldestPendingAt: snapshot.OldestPendingAt, RetriedEventCount: snapshot.RetriedEventCount,
		LastDeliveryError: snapshot.LastDeliveryError, LastDeliveryErrorAt: snapshot.LastDeliveryErrorAt,
		LastDeliverySuccess: snapshot.LastDeliverySuccess,
	}
}

func (component *ObservationOwner) RecordObservationFailure(at time.Time) {
	component.application.RecordFailure(at)
}

func (component *ObservationOwner) WriteObservation(ctx context.Context, handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	component.writeObservation(ctx, handle, actor, tokenID, runtimeID, action, payload)
}

func (component *ObservationOwner) WriteObservationRequired(ctx context.Context, handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload any) error {
	return component.writeObservationRequired(ctx, handle, actor, tokenID, runtimeID, action, payload)
}

func (component *ObservationOwner) PrepareObservationRedactor(ctx context.Context, handle *WorkspaceHandle) func(string) string {
	runtime, _ := component.observationWorkspace(handle)
	return component.application.PrepareRedactor(ctx, runtime)
}

type observationMutationRunner func(context.Context, string, func() any, func(*sql.Tx) error) error

func (component *ObservationOwner) mutationRunner(handle *WorkspaceHandle, actor string, tokenID *int64, runtimeID int64) observationMutationRunner {
	return component.observationMutationRunner(handle, actor, tokenID, runtimeID)
}

func (component *ObservationOwner) ProjectObservations(ctx context.Context, handle *WorkspaceHandle) {
	if runtime, ok := component.observationWorkspace(handle); ok {
		component.application.Project(ctx, runtime)
	}
}

type ObservationHTTPHandlers struct {
	Retention Retention
	History   History
	Audit     Audit
}

type Retention interface {
	Get(http.ResponseWriter, *http.Request)
	Update(http.ResponseWriter, *http.Request)
	Purge(http.ResponseWriter, *http.Request)
}

type History interface {
	ListTargetFacets(http.ResponseWriter, *http.Request)
	ListEntries(http.ResponseWriter, *http.Request)
	GetEntry(http.ResponseWriter, *http.Request)
	AttachEntryLabel(http.ResponseWriter, *http.Request)
	DetachEntryLabel(http.ResponseWriter, *http.Request)
	ListLabels(http.ResponseWriter, *http.Request)
	CreateLabel(http.ResponseWriter, *http.Request)
	DeleteLabel(http.ResponseWriter, *http.Request)
}

type Audit interface {
	List(http.ResponseWriter, *http.Request)
	Get(http.ResponseWriter, *http.Request)
}

func (component *ObservationOwner) ObservationHTTPHandlers(active func(http.ResponseWriter) (*WorkspaceHandle, bool)) ObservationHTTPHandlers {
	handlers := component.application.HTTPHandlers(func(w http.ResponseWriter) (observationapp.Runtime, bool) {
		handle, ok := active(w)
		if !ok {
			return observationapp.Runtime{}, false
		}
		return component.observationWorkspace(handle)
	})
	return ObservationHTTPHandlers{Retention: handlers.Retention, History: handlers.History, Audit: handlers.Audit}
}

func (component *ObservationOwner) ObservationDiagnostics(ctx context.Context, handle *WorkspaceHandle) (json.RawMessage, error) {
	runtime, _ := component.observationWorkspace(handle)
	report, err := component.application.Diagnostics(ctx, runtime)
	if err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

func (component *ObservationOwner) PrepareDiagnosticsDownload(w http.ResponseWriter) string {
	return component.application.PrepareDiagnosticsDownload(w)
}

func (component *ObservationOwner) vaultRequestStoreFactory(handle *WorkspaceHandle) gatewayvault.RequestStoreFactory {
	runtime, _ := component.observationRuntime(handle)
	return gatewayvault.RequestStoreFactory(component.application.VaultRequestStoreFactory(runtime))
}

func (component *ObservationOwner) syncVaultActionRequest(ctx context.Context, handle *WorkspaceHandle, id int64) error {
	runtime, _ := component.observationRuntime(handle)
	return component.application.SyncVaultActionRequest(ctx, runtime, id)
}
