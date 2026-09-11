package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (component *Component) ProfileDeletionScope(w http.ResponseWriter) (connectormanagement.ProfileDeletionScope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.ProfileDeletionScope{}, false
	}
	return connectormanagement.ProfileDeletionScope{
		Database: runtime.StoragePort().DatabaseHandle(), AcquireExclusive: runtime.SecurityPort().VaultDeliveryCoordinator().AcquireExclusive,
		Cleanup: func(ctx context.Context, target connectortargets.Target, profile connectortargets.CredentialProfile) (connectormanagement.ProfileCleanupOutcome, error) {
			return connectormanagement.CleanupProvisionedCredentialProfileIfNeeded(ctx, connectormanagement.ManagedCredentialCleanupScope{
				Database: runtime.StoragePort().DatabaseHandle(), Registry: runtime.ConnectorPort().ConnectorRegistry(), Runtime: component.dependencies.Credentials.Runtime(runtime),
			}, target, profile)
		},
		BeforeDelete: func(ctx context.Context, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.dependencies.Credentials.BeforeDelete(ctx, runtime, target, profile)
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return component.dependencies.Runtime.Transaction(ctx, runtime, mutate)
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return component.dependencies.Lifecycle.AfterChange(ctx, runtime, change)
		},
	}, true
}

func (component *Component) ProfileTestingScope(w http.ResponseWriter) (connectormanagement.ProfileTestingScope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.ProfileTestingScope{}, false
	}
	return connectormanagement.ProfileTestingScope{
		Database: runtime.StoragePort().DatabaseHandle(), Registry: runtime.ConnectorPort().ConnectorRegistry(), Runtime: component.dependencies.Credentials.Runtime(runtime),
		SpecialTest: func(w http.ResponseWriter, r *http.Request, target connectors.TargetView, profile connectors.CredentialProfileView) bool {
			return component.dependencies.Credentials.SpecialTest(w, r, runtime, target, profile)
		},
		RedactDetails: func(ctx context.Context, details map[string]any, boundary connectormanagement.CredentialBoundary) (map[string]any, error) {
			return component.dependencies.Credentials.RedactDetails(ctx, runtime, details, boundary)
		},
	}, true
}

func (component *Component) ProfileBackupScope(w http.ResponseWriter) (connectormanagement.ProfileBackupScope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.ProfileBackupScope{}, false
	}
	return connectormanagement.ProfileBackupScope{
		Database: runtime.StoragePort().DatabaseHandle(), Registry: runtime.ConnectorPort().ConnectorRegistry(), Runtime: component.dependencies.Credentials.Runtime(runtime),
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			component.dependencies.Observation.Observe(ctx, runtime, action, payload)
		},
	}, true
}

func (component *Component) HostPingScope(w http.ResponseWriter) (connectormanagement.HostPingScope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.HostPingScope{}, false
	}
	return connectormanagement.HostPingScope{
		ValidateTransport: func(ctx context.Context, projectID int64, mode, ref string) error {
			return ValidateTransport(ctx, connectortargets.NewStore(runtime.StoragePort().DatabaseHandle()), projectID, map[string]any{"connection_mode": mode, "transport_target_ref": ref}, component.dependencies.Capabilities.HasTCPTransport)
		},
		Probe: func(ctx context.Context, request connectors.NetworkDialRequest) error {
			return component.dependencies.Network.Probe(ctx, runtime, request)
		},
		Redact: func(ctx context.Context, value string) string {
			return component.dependencies.Network.Redact(ctx, runtime, value)
		},
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			component.dependencies.Observation.Observe(ctx, runtime, action, payload)
		},
	}, true
}
