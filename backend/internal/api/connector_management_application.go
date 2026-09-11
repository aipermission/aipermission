package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

type provisionConnectorCredentialProfileRequest = connectormgmt.ProvisionRequest

func (s *Server) connectorCredentialResourceDependencies() connectormgmt.CredentialResourceDependencies {
	return connectormgmt.CredentialResourceDependencies{
		Adapter: func(kind string) connectormgmt.CredentialResourceAdapter {
			return s.connectorCredentialResourceAdapterFor(kind)
		},
		Runtime:    connectorapi.PortCredentialResourceRuntime,
		WriteError: writeError,
	}
}

func (s *Server) connectorManagementApplication() *connectormgmt.Component {
	return connectormgmt.New(connectormgmt.Dependencies{
		Runtime: connectormgmt.RuntimeDependencies{
			Active: s.activeRuntimeOrLocked,
			Transaction: func(ctx context.Context, runtime gatewayinfra.Runtime, mutate func(*sql.Tx, connectormgmt.AuditAppender) error) error {
				return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
					return mutate(tx, connectormgmt.AuditAppender(appendAudit))
				})
			},
		},
		Capabilities: connectormgmt.CapabilityDependencies{
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
			SessionEnvironment: func(ctx context.Context, runtime gatewayinfra.Runtime, id int64) bool {
				return requireSessionEnvironmentCapability(ctx, s, runtime, id) == nil
			},
		},
		Credentials: connectormgmt.CredentialDependencies{
			Preparation: s.connectorCredentialPreparationPorts,
			Runtime:     s.connectorCredentialRuntimePorts,
			BeforeCreate: func(ctx context.Context, runtime gatewayinfra.Runtime, target connectormgmt.Target) error {
				if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
					return adapter.BeforeCreateCredentialProfile(ctx, s.connectorTargetLifecycleRuntime(runtime, target.ConnectorKind), target)
				}
				return nil
			},
			BeforeDelete: func(ctx context.Context, runtime gatewayinfra.Runtime, target connectormgmt.Target, profile connectormgmt.CredentialProfile) error {
				if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
					gateway, _ := s.connectorPortsApplication().RuntimeActionPorts(runtime, target.ConnectorKind)
					return adapter.BeforeDeleteCredentialProfile(ctx, gateway, s.connectorTargetLifecycleRuntime(runtime, target.ConnectorKind), target, profile)
				}
				return nil
			},
			SpecialTest: func(w http.ResponseWriter, r *http.Request, runtime gatewayinfra.Runtime, target connectors.TargetView, profile connectors.CredentialProfileView) bool {
				adapter := s.connectorCredentialProfileTesterFor(target.ConnectorKind)
				if adapter == nil {
					return false
				}
				adapter.TestCredentialProfile(s.connectorPortsApplication().PeerGateway(), w, r, connectorDataRuntimePort(runtime, target.ConnectorKind), target, profile)
				return true
			},
			RedactDetails: func(ctx context.Context, runtime gatewayinfra.Runtime, details map[string]any, boundary connectormgmt.CredentialBoundary) (map[string]any, error) {
				redacted, err := s.redactedConnectorValueWithCredentialBoundary(ctx, runtime, details, connectorSensitiveOutputFields(), nil, boundary)
				if err != nil || redacted == nil {
					return nil, err
				}
				if typed, ok := redacted.(map[string]any); ok {
					return typed, nil
				}
				return map[string]any{"value": redacted}, nil
			},
		},
		Lifecycle: connectormgmt.LifecycleDependencies{
			AfterChange: func(ctx context.Context, runtime gatewayinfra.Runtime, change connectormgmt.TargetLifecycleChange) error {
				return (connectorTargetHandlers{s}).afterConnectorCredentialLifecycleChange(ctx, runtime, change.TargetID, change.ProfileID, change.StaleReason, change.UserMessage, change.IncludeRunning)
			},
		},
		Network: connectormgmt.NetworkDependencies{
			Probe: func(ctx context.Context, runtime gatewayinfra.Runtime, request connectors.NetworkDialRequest) error {
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
			Redact: func(ctx context.Context, runtime gatewayinfra.Runtime, value string) string {
				return s.redactForPersistence(ctx, runtime, value)
			},
		},
		Observation: connectormgmt.ObservationDependencies{
			Observe: func(ctx context.Context, runtime gatewayinfra.Runtime, action string, payload any) {
				s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
			},
			AuditRequired: func(ctx context.Context, runtime gatewayinfra.Runtime, action string, payload any) error {
				return s.writeAuditRequired(ctx, runtime, "gateway", nil, 0, action, payload)
			},
		},
	})
}
