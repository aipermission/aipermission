package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	applicationmanagement "github.com/aipermission/aipermission/backend/internal/applicationconnectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type provisionConnectorCredentialProfileRequest = connectormanagement.ProvisionRequest

func (s *Server) connectorCredentialResourceDependencies() applicationmanagement.CredentialResourceDependencies {
	return applicationmanagement.CredentialResourceDependencies{
		Adapter:    s.connectorCredentialResourceAdapterFor,
		WriteError: writeError,
	}
}

func (s *Server) connectorManagementApplication() *applicationmanagement.Component {
	return applicationmanagement.New(applicationmanagement.Dependencies{
		ActiveRuntime: s.activeRuntimeOrLocked,
		LiveConsoleKind: func(kind string) (string, bool) {
			adapter := s.connectorLiveConsoleTargetAdapterFor(kind)
			if adapter == nil {
				return "", false
			}
			return adapter.LiveConsoleCapabilityKind(), true
		},
		HasFileTransfer: func(kind string) bool { return s.connectorFileTransferAdapterFor(kind) != nil },
		HasTCPTransport: func(kind string) bool {
			adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.TCPTransportAdapter)
			return adapter != nil
		},
		SessionEnvironment: func(ctx context.Context, runtime *workspaceruntime.Runtime, id int64) bool {
			return requireSessionEnvironmentCapability(ctx, s, runtime, id) == nil
		},
		Preparation:       s.connectorCredentialPreparationPorts,
		CredentialRuntime: s.connectorCredentialRuntimePorts,
		Transaction: func(ctx context.Context, runtime *workspaceruntime.Runtime, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, connectormanagement.AuditAppender(appendAudit))
			})
		},
		AfterLifecycle: func(ctx context.Context, runtime *workspaceruntime.Runtime, change connectormanagement.TargetLifecycleChange) error {
			return (connectorTargetHandlers{s}).afterConnectorCredentialLifecycleChange(ctx, runtime, change.TargetID, change.ProfileID, change.StaleReason, change.UserMessage, change.IncludeRunning)
		},
		BeforeCreate: func(ctx context.Context, runtime *workspaceruntime.Runtime, target connectortargets.Target) error {
			if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
				return adapter.BeforeCreateCredentialProfile(ctx, connectorTargetLifecycleRuntime(runtime, target.ConnectorKind), target)
			}
			return nil
		},
		BeforeDelete: func(ctx context.Context, runtime *workspaceruntime.Runtime, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
				gateway := connectorRuntimeActionGatewayPort{connectorPeerGatewayPort: connectorPeerGatewayPort{server: s}, runtime: runtime, kind: target.ConnectorKind}
				return adapter.BeforeDeleteCredentialProfile(ctx, gateway, connectorTargetLifecycleRuntime(runtime, target.ConnectorKind), target, profile)
			}
			return nil
		},
		SpecialTest: func(w http.ResponseWriter, r *http.Request, runtime *workspaceruntime.Runtime, target connectors.TargetView, profile connectors.CredentialProfileView) bool {
			adapter := s.connectorCredentialProfileTesterFor(target.ConnectorKind)
			if adapter == nil {
				return false
			}
			adapter.TestCredentialProfile(connectorPeerGatewayPort{server: s}, w, r, connectorDataRuntimePort(runtime, target.ConnectorKind), target, profile)
			return true
		},
		RedactDetails: func(ctx context.Context, runtime *workspaceruntime.Runtime, details map[string]any, boundary connectormanagement.CredentialBoundary) (map[string]any, error) {
			redacted, err := s.redactedConnectorValueWithCredentialBoundary(ctx, runtime, details, connectorSensitiveOutputFields(), nil, boundary)
			if err != nil || redacted == nil {
				return nil, err
			}
			if typed, ok := redacted.(map[string]any); ok {
				return typed, nil
			}
			return map[string]any{"value": redacted}, nil
		},
		Probe: func(ctx context.Context, runtime *workspaceruntime.Runtime, request connectors.NetworkDialRequest) error {
			connection, err := (connectorNetworkTransport{server: s, runtime: runtime}).DialConnectorTCP(ctx, request)
			if err != nil {
				return err
			}
			if connection == nil {
				return errors.New("connector transport returned no connection")
			}
			_ = connection.Close()
			return nil
		},
		Redact: func(ctx context.Context, runtime *workspaceruntime.Runtime, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Observe: func(ctx context.Context, runtime *workspaceruntime.Runtime, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
		AuditRequired: func(ctx context.Context, runtime *workspaceruntime.Runtime, action string, payload any) error {
			return s.writeAuditRequired(ctx, runtime, "gateway", nil, 0, action, payload)
		},
	})
}
