package api

import (
	"context"
	"encoding/json"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

type provisionConnectorCredentialProfileRequest = connectormgmt.ProvisionRequest

func (s *Server) connectorCredentialResourceDependencies() connectormgmt.CredentialResourceDependencies {
	return connectormgmt.CredentialResourceDependencies{
		Adapter: func(kind string) connectormgmt.CredentialResourceAdapter {
			return s.connectorRuntime.CredentialResourceAdapter(kind)
		},
		WriteError: writeError,
	}
}

func (s *Server) connectorManagementApplication() *connectormgmt.Component {
	if s == nil || s.connectorManagement == nil {
		panic("connector management component is not initialized")
	}
	return s.connectorManagement
}

func (s *Server) newConnectorManagementApplication() *connectormgmt.Component {
	return connectormgmt.New(connectormgmt.Dependencies{
		Approvals: s.connectorApprovalHTTPScope,
		Active: func(w http.ResponseWriter) (connectormgmt.Workspace, bool) {
			runtime, ok := s.activeRuntimeOrLocked(w)
			if !ok {
				return connectormgmt.Workspace{}, false
			}
			return s.connectorManagementWorkspace(runtime), true
		},
		Capabilities: connectormgmt.CapabilityDependencies{
			LiveConsoleKind: s.connectorRuntime.LiveConsoleCapabilityKind,
			HasFileTransfer: s.connectorRuntime.HasFileTransfer,
			HasTCPTransport: s.connectorRuntime.HasTCPTransport,
		},
		Adapters:     s.connectorAdapterRegistry(),
		PeerIdentity: s.connectorRuntime.PeerGateway(),
	})
}

func (s *Server) connectorCatalog(runtime *gatewayinfra.WorkspaceHandle) connectormgmt.Catalog {
	return s.connectorManagementOwner.ConnectorCatalog(runtime, s.connectorManagementApplication())
}

func (s *Server) connectorManagementWorkspace(runtime *gatewayinfra.WorkspaceHandle) connectormgmt.Workspace {
	preparation := s.connectorCredentialPreparationPorts(runtime)
	workspace, _ := s.connectorManagementOwner.ConnectorManagementWorkspace(runtime, connectormgmt.Workspace{
		Storage: connectormgmt.StoragePorts{
			EncryptSecret: func(ctx context.Context, id int64, raw json.RawMessage) (string, error) {
				secret := map[string]any{}
				if err := json.Unmarshal(raw, &secret); err != nil {
					return "", err
				}
				return preparation.Encrypt(ctx, id, secret)
			},
		},
		Credentials: connectormgmt.CredentialPorts{
			Preparation: preparation,
			Runtime:     s.connectorCredentialRuntimePorts(runtime),
			SessionEnvironment: func(ctx context.Context, id int64) bool {
				return requireSessionEnvironmentCapability(ctx, s, runtime, id) == nil
			},
			BeforeCreate: func(ctx context.Context, target connectormgmt.Target) error {
				if adapter := s.connectorRuntime.CredentialProfileLifecycleAdapter(target.ConnectorKind); adapter != nil {
					return adapter.BeforeCreateCredentialProfile(ctx, s.connectorRuntime.TargetLifecycleRuntime(runtime, target.ConnectorKind), target)
				}
				return nil
			},
			BeforeDelete: func(ctx context.Context, target connectormgmt.Target, profile connectormgmt.CredentialProfile) error {
				if adapter := s.connectorRuntime.CredentialProfileLifecycleAdapter(target.ConnectorKind); adapter != nil {
					gateway, _ := s.connectorRuntime.RuntimeActionPorts(runtime, target.ConnectorKind)
					return adapter.BeforeDeleteCredentialProfile(ctx, gateway, s.connectorRuntime.TargetLifecycleRuntime(runtime, target.ConnectorKind), target, profile)
				}
				return nil
			},
			SpecialTest: func(w http.ResponseWriter, r *http.Request, target connectors.TargetView, profile connectors.CredentialProfileView) bool {
				adapter := s.connectorRuntime.CredentialProfileTester(target.ConnectorKind)
				if adapter == nil {
					return false
				}
				adapter.TestCredentialProfile(s.connectorRuntime.PeerGateway(), w, r, s.connectorRuntime.DataRuntime(runtime, target.ConnectorKind), target, profile)
				return true
			},
			RedactDetails: func(ctx context.Context, details map[string]any, boundary connectormgmt.CredentialBoundary) (map[string]any, error) {
				redacted, err := s.redactedConnectorValueWithCredentialBoundary(ctx, runtime, details, s.connectorSensitiveOutputFields(), nil, gatewayactions.AdoptCredentialBoundary(boundary))
				if err != nil || redacted == nil {
					return nil, err
				}
				if typed, ok := redacted.(map[string]any); ok {
					return typed, nil
				}
				return map[string]any{"value": redacted}, nil
			},
			ResourceRuntime: func(kind string) connectorapi.CredentialResourceRuntime {
				return s.connectorRuntime.CredentialResourceRuntime(runtime, kind)
			},
		},
		Lifecycle: connectormgmt.LifecyclePorts{
			AfterChange: func(ctx context.Context, change connectormgmt.TargetLifecycleChange) error {
				return s.connectorLifecycleApplication(runtime).AfterCredentialChange(ctx, change)
			},
			DeleteTarget: func(ctx context.Context, target connectormgmt.Target, payload map[string]any) error {
				return s.connectorLifecycleApplication(runtime).DeleteTarget(ctx, target, payload)
			},
			FinalizeTarget: func(ctx context.Context, target connectormgmt.Target, reason string) (int64, error) {
				return s.connectorLifecycleApplication(runtime).FinalizeDeletedTarget(ctx, target, reason)
			},
		},
		Adapters: connectormgmt.TargetAdapterPorts{
			DataRuntime: func(kind string) connectorapi.ConnectorDataRuntime {
				return s.connectorRuntime.DataRuntime(runtime, kind)
			},
			LifecycleRuntime: func(kind string) connectorapi.TargetLifecycleRuntime {
				return s.connectorRuntime.TargetLifecycleRuntime(runtime, kind)
			},
			DeletionGateway:  s.connectorRuntime.TargetDeletionGateway(runtime),
			OperationGateway: s.connectorRuntime.TargetOperationGateway(runtime),
		},
		Network: connectormgmt.NetworkPorts{
			Probe: func(ctx context.Context, request connectors.NetworkDialRequest) error {
				return s.connectorRuntime.NetworkProbe(ctx, runtime, request)
			},
			Redact: func(ctx context.Context, value string) string {
				return s.redactForPersistence(ctx, runtime, value)
			},
		},
		Observation: connectormgmt.ObservationPorts{
			Observe: func(ctx context.Context, action string, payload any) {
				s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
			},
			AuditRequired: func(ctx context.Context, action string, payload any) error {
				return s.writeAuditRequired(ctx, runtime, "gateway", nil, 0, action, payload)
			},
		},
	})
	return workspace
}
