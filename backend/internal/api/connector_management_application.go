package api

import (
	"context"
	"fmt"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

type provisionConnectorCredentialProfileRequest = connectormgmt.ProvisionRequest

func (s *Server) connectorManagementApplication() *gatewayinfra.ConnectorManagementApplication {
	if s == nil || s.connectorManagement == nil {
		panic("connector management application is not initialized")
	}
	return s.connectorManagement
}

func (s *Server) newConnectorManagementApplication() *gatewayinfra.ConnectorManagementApplication {
	application, err := gatewayinfra.NewConnectorManagementApplication(
		s.connectorManagementOwner,
		s.connectorRuntime,
		gatewayinfra.ConnectorManagementPorts{
			Active:    s.activeRuntimeOrLocked,
			Approvals: s.connectorApprovalHTTPScope,
			SessionEnvironment: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, runtimeID int64) bool {
				return requireSessionEnvironmentCapability(ctx, s, runtime, runtimeID) == nil
			},
			RedactDetails: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, details map[string]any, boundary connectormgmt.CredentialBoundary) (map[string]any, error) {
				redacted, err := s.redactedConnectorValueWithCredentialBoundary(ctx, runtime, details, s.connectorSensitiveOutputFields(), nil, gatewayactions.AdoptCredentialBoundary(boundary))
				if err != nil || redacted == nil {
					return nil, err
				}
				if typed, ok := redacted.(map[string]any); ok {
					return typed, nil
				}
				return map[string]any{"value": redacted}, nil
			},
			RedactResult: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, result connectors.ActionResult, boundary connectormgmt.CredentialBoundary) (connectors.ActionResult, error) {
				return s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, gatewayactions.AdoptCredentialBoundary(boundary))
			},
			Redact: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, value string) string {
				return s.redactForPersistence(ctx, runtime, value)
			},
			InvalidateVault: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, targetID, profileID int64, reason string) error {
				lifecycle, err := s.vaultSessionLifecycle(runtime)
				if err != nil {
					return err
				}
				return lifecycle.InvalidateTargetProfile(ctx, targetID, profileID, reason)
			},
			Observe: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, action string, payload any) {
				s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
			},
			AuditRequired: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, action string, payload any) error {
				return s.writeAuditRequired(ctx, runtime, "gateway", nil, 0, action, payload)
			},
			WriteError: writeError,
		},
	)
	if err != nil {
		panic(fmt.Sprintf("initialize connector management application: %v", err))
	}
	return application
}

func (s *Server) connectorCatalog(runtime *gatewayinfra.WorkspaceHandle) connectormgmt.Catalog {
	return s.connectorManagementApplication().Catalog(runtime)
}
