package gatewayconnectormanagement

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (component *Component) ProfileDeletionScope(w http.ResponseWriter) (connectormanagement.ProfileDeletionScope, bool) {
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.ProfileDeletionScope{}, false
	}
	return connectormanagement.ProfileDeletionScope{
		Database: workspace.Storage.Database, AcquireExclusive: workspace.Storage.AcquireExclusive,
		Cleanup: func(ctx context.Context, target connectortargets.Target, profile connectortargets.CredentialProfile) (connectormanagement.ProfileCleanupOutcome, error) {
			return connectormanagement.CleanupProvisionedCredentialProfileIfNeeded(ctx, connectormanagement.ManagedCredentialCleanupScope{
				Database: workspace.Storage.Database, Registry: workspace.Storage.Registry, Runtime: workspace.Credentials.Runtime.domain(),
			}, target, profile)
		},
		BeforeDelete: func(ctx context.Context, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return workspace.Credentials.BeforeDelete(ctx, targetFromDomain(target), credentialProfileFromDomain(profile))
		},
		WithTransaction: adaptTransaction(workspace.Storage.Transaction),
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return workspace.Lifecycle.AfterChange(ctx, TargetLifecycleChange(change))
		},
	}, true
}

func (component *Component) ProfileTestingScope(w http.ResponseWriter) (connectormanagement.ProfileTestingScope, bool) {
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.ProfileTestingScope{}, false
	}
	return connectormanagement.ProfileTestingScope{
		Database: workspace.Storage.Database, Registry: workspace.Storage.Registry, Runtime: workspace.Credentials.Runtime.domain(),
		SpecialTest: func(w http.ResponseWriter, r *http.Request, target connectors.TargetView, profile connectors.CredentialProfileView) bool {
			return workspace.Credentials.SpecialTest(w, r, target, profile)
		},
		RedactDetails: func(ctx context.Context, details map[string]any, boundary connectormanagement.CredentialBoundary) (map[string]any, error) {
			return workspace.Credentials.RedactDetails(ctx, details, wrapCredentialBoundary(boundary))
		},
	}, true
}

func (component *Component) ProfileBackupScope(w http.ResponseWriter) (connectormanagement.ProfileBackupScope, bool) {
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.ProfileBackupScope{}, false
	}
	return connectormanagement.ProfileBackupScope{
		Database: workspace.Storage.Database, Registry: workspace.Storage.Registry, Runtime: workspace.Credentials.Runtime.domain(),
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			workspace.Observation.Observe(ctx, action, payload)
		},
	}, true
}

func (component *Component) HostPingScope(w http.ResponseWriter) (connectormanagement.HostPingScope, bool) {
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.HostPingScope{}, false
	}
	return connectormanagement.HostPingScope{
		ValidateTransport: func(ctx context.Context, projectID int64, mode, ref string) error {
			return ValidateTransport(ctx, connectortargets.NewStore(workspace.Storage.Database), projectID, map[string]any{"connection_mode": mode, "transport_target_ref": ref}, component.dependencies.Capabilities.HasTCPTransport)
		},
		Probe: func(ctx context.Context, request connectors.NetworkDialRequest) error {
			return workspace.Network.Probe(ctx, request)
		},
		Redact: func(ctx context.Context, value string) string {
			return workspace.Network.Redact(ctx, value)
		},
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			workspace.Observation.Observe(ctx, action, payload)
		},
	}, true
}
