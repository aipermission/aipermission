package gatewayinfrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

type ConnectorManagementPorts struct {
	Active             func(http.ResponseWriter) (*WorkspaceHandle, bool)
	Approvals          connectormgmt.ConnectorApprovalScopeProvider
	SessionEnvironment func(context.Context, *WorkspaceHandle, int64) bool
	RedactDetails      func(context.Context, *WorkspaceHandle, map[string]any, connectormgmt.CredentialBoundary) (map[string]any, error)
	RedactResult       func(context.Context, *WorkspaceHandle, connectors.ActionResult, connectormgmt.CredentialBoundary) (connectors.ActionResult, error)
	Redact             func(context.Context, *WorkspaceHandle, string) string
	InvalidateVault    func(context.Context, *WorkspaceHandle, int64, int64, string) error
	Observe            func(context.Context, *WorkspaceHandle, string, any)
	AuditRequired      func(context.Context, *WorkspaceHandle, string, any) error
	WriteError         func(http.ResponseWriter, int, string)
}

// ConnectorManagementApplication owns connector target, profile, credential,
// and lifecycle composition. It prevents HTTP transports from assembling a
// storage-bearing management workspace.
type ConnectorManagementApplication struct {
	owner   *ConnectorManagementOwner
	runtime *ConnectorRuntimeApplication
	ports   ConnectorManagementPorts
	core    *connectormgmt.Component
}

func NewConnectorManagementApplication(owner *ConnectorManagementOwner, runtime *ConnectorRuntimeApplication, ports ConnectorManagementPorts) (*ConnectorManagementApplication, error) {
	if owner == nil || runtime == nil {
		return nil, errors.New("connector management owners are required")
	}
	if ports.Active == nil || ports.Approvals == nil || ports.SessionEnvironment == nil ||
		ports.RedactDetails == nil || ports.RedactResult == nil || ports.Redact == nil ||
		ports.InvalidateVault == nil || ports.Observe == nil || ports.AuditRequired == nil || ports.WriteError == nil {
		return nil, errors.New("connector management ports are incomplete")
	}
	application := &ConnectorManagementApplication{owner: owner, runtime: runtime, ports: ports}
	application.core = connectormgmt.New(connectormgmt.Dependencies{
		Approvals: ports.Approvals,
		Active: func(w http.ResponseWriter) (connectormgmt.Workspace, bool) {
			if ports.Active == nil {
				return connectormgmt.Workspace{}, false
			}
			handle, ok := ports.Active(w)
			if !ok {
				return connectormgmt.Workspace{}, false
			}
			return application.workspace(handle)
		},
		Capabilities: connectormgmt.CapabilityDependencies{
			LiveConsoleKind: runtime.LiveConsoleCapabilityKind,
			HasFileTransfer: runtime.HasFileTransfer,
			HasTCPTransport: runtime.HasTCPTransport,
		},
		Adapters:     runtime.adapters,
		PeerIdentity: runtime.PeerGateway(),
	})
	return application, nil
}

func (application *ConnectorManagementApplication) HTTPHandlers() connectormgmt.HTTPHandlers {
	return application.core.HTTPHandlers()
}

func (application *ConnectorManagementApplication) CredentialResources() connectormgmt.CredentialResourceHandlers {
	return application.core.CredentialResources(connectormgmt.CredentialResourceDependencies{
		Adapter: func(kind string) connectormgmt.CredentialResourceAdapter {
			return application.runtime.CredentialResourceAdapter(kind)
		},
		WriteError: application.ports.WriteError,
	})
}

func (application *ConnectorManagementApplication) Catalog(handle *WorkspaceHandle) connectormgmt.Catalog {
	return application.owner.connectorCatalog(handle, application.core)
}

func (application *ConnectorManagementApplication) CredentialPreparation(handle *WorkspaceHandle) connectormgmt.CredentialPreparationPorts {
	return application.core.RuntimeCredentialPreparation(application.owner.connectorCredentialStorage(handle), func(kind string) connectormgmt.CredentialCanonicalizer {
		adapter := application.runtime.CredentialCanonicalizer(kind)
		if adapter == nil {
			return nil
		}
		return func(ctx context.Context, _, credentialKind string, public map[string]any) (map[string]any, error) {
			return adapter.CanonicalCredentialPublic(ctx, application.runtime.DataRuntime(handle, kind), credentialKind, public)
		}
	})
}

func (application *ConnectorManagementApplication) credentialRuntime(handle *WorkspaceHandle) connectormgmt.CredentialRuntimePorts {
	return application.core.RuntimeCredentialPorts(
		application.owner.connectorCredentialStorage(handle),
		func(kind string) connectors.RuntimeCapabilityResolver {
			return application.runtime.RuntimeCapabilities(handle, kind)
		},
		func(ctx context.Context, result connectors.ActionResult, boundary connectormgmt.CredentialBoundary) (connectors.ActionResult, error) {
			return application.ports.RedactResult(ctx, handle, result, boundary)
		},
		func(ctx context.Context, value string) string { return application.redact(ctx, handle, value) },
	)
}

func (application *ConnectorManagementApplication) lifecycle(handle *WorkspaceHandle) *connectormgmt.LifecycleService {
	return connectormgmt.NewLifecycleService(connectormgmt.LifecycleServiceDependencies{
		Mutate: application.owner.lifecycleMutationRunner(handle),
		Redact: func(ctx context.Context, value string) string {
			return application.redact(ctx, handle, value)
		},
		InvalidateVault: func(ctx context.Context, targetID, profileID int64, reason string) error {
			return application.ports.InvalidateVault(ctx, handle, targetID, profileID, reason)
		},
	})
}

func (application *ConnectorManagementApplication) DeleteTarget(ctx context.Context, handle *WorkspaceHandle, target connectormgmt.Target, payload map[string]any) error {
	return application.lifecycle(handle).DeleteTarget(ctx, target, payload)
}

func (application *ConnectorManagementApplication) FinalizeDeletedTarget(ctx context.Context, handle *WorkspaceHandle, target connectormgmt.Target, reason string) (int64, error) {
	return application.lifecycle(handle).FinalizeDeletedTarget(ctx, target, reason)
}

func (application *ConnectorManagementApplication) AfterCredentialChange(ctx context.Context, handle *WorkspaceHandle, change connectormgmt.TargetLifecycleChange) error {
	return application.lifecycle(handle).AfterCredentialChange(ctx, change)
}

func (application *ConnectorManagementApplication) ConnectorApprovalItemFromRequest(item connectormgmt.ActionRequest) connectormgmt.ConnectorApprovalItem {
	return application.core.ConnectorApprovalItemFromRequest(item)
}

func (application *ConnectorManagementApplication) ConnectorApprovalItemForResponse(ctx context.Context, workflow connectormgmt.ConnectorApprovalWorkflow, item connectormgmt.ActionRequest) (connectormgmt.ConnectorApprovalItem, error) {
	return application.core.ConnectorApprovalItemForResponse(ctx, workflow, item)
}

func (application *ConnectorManagementApplication) workspace(handle *WorkspaceHandle) (connectormgmt.Workspace, bool) {
	preparation := application.CredentialPreparation(handle)
	workspace := connectormgmt.Workspace{
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
			Runtime:     application.credentialRuntime(handle),
			SessionEnvironment: func(ctx context.Context, id int64) bool {
				return application.ports.SessionEnvironment(ctx, handle, id)
			},
			BeforeCreate: func(ctx context.Context, target connectormgmt.Target) error {
				adapter := application.runtime.CredentialProfileLifecycleAdapter(target.ConnectorKind)
				if adapter == nil {
					return nil
				}
				return adapter.BeforeCreateCredentialProfile(ctx, application.runtime.TargetLifecycleRuntime(handle, target.ConnectorKind), connectorTarget(target))
			},
			BeforeDelete: func(ctx context.Context, target connectormgmt.Target, profile connectormgmt.CredentialProfile) error {
				adapter := application.runtime.CredentialProfileLifecycleAdapter(target.ConnectorKind)
				if adapter == nil {
					return nil
				}
				gateway, _ := application.runtime.RuntimeActionPorts(handle, target.ConnectorKind)
				return adapter.BeforeDeleteCredentialProfile(ctx, gateway, application.runtime.TargetLifecycleRuntime(handle, target.ConnectorKind), connectorTarget(target), connectorCredentialProfile(profile))
			},
			SpecialTest: func(w http.ResponseWriter, r *http.Request, target connectors.TargetView, profile connectors.CredentialProfileView) bool {
				adapter := application.runtime.CredentialProfileTester(target.ConnectorKind)
				if adapter == nil {
					return false
				}
				adapter.TestCredentialProfile(application.runtime.PeerGateway(), w, r, application.runtime.DataRuntime(handle, target.ConnectorKind), target, profile)
				return true
			},
			RedactDetails: func(ctx context.Context, details map[string]any, boundary connectormgmt.CredentialBoundary) (map[string]any, error) {
				return application.ports.RedactDetails(ctx, handle, details, boundary)
			},
			ResourceRuntime: func(kind string) connectorapi.CredentialResourceRuntime {
				return application.runtime.CredentialResourceRuntime(handle, kind)
			},
		},
		Lifecycle: connectormgmt.LifecyclePorts{
			AfterChange: func(ctx context.Context, change connectormgmt.TargetLifecycleChange) error {
				return application.AfterCredentialChange(ctx, handle, change)
			},
			DeleteTarget: func(ctx context.Context, target connectormgmt.Target, payload map[string]any) error {
				return application.DeleteTarget(ctx, handle, target, payload)
			},
			FinalizeTarget: func(ctx context.Context, target connectormgmt.Target, reason string) (int64, error) {
				return application.FinalizeDeletedTarget(ctx, handle, target, reason)
			},
		},
		Adapters: connectormgmt.TargetAdapterPorts{
			DataRuntime: func(kind string) connectorapi.ConnectorDataRuntime {
				return application.runtime.DataRuntime(handle, kind)
			},
			LifecycleRuntime: func(kind string) connectorapi.TargetLifecycleRuntime {
				return application.runtime.TargetLifecycleRuntime(handle, kind)
			},
			DeletionGateway:  application.runtime.TargetDeletionGateway(handle),
			OperationGateway: application.runtime.TargetOperationGateway(handle),
		},
		Network: connectormgmt.NetworkPorts{
			Probe: func(ctx context.Context, request connectors.NetworkDialRequest) error {
				return application.runtime.NetworkProbe(ctx, handle, request)
			},
			Redact: func(ctx context.Context, value string) string { return application.redact(ctx, handle, value) },
		},
		Observation: connectormgmt.ObservationPorts{
			Observe: func(ctx context.Context, action string, payload any) {
				application.ports.Observe(ctx, handle, action, payload)
			},
			AuditRequired: func(ctx context.Context, action string, payload any) error {
				return application.ports.AuditRequired(ctx, handle, action, payload)
			},
		},
	}
	return application.owner.connectorManagementWorkspace(handle, workspace)
}

func (application *ConnectorManagementApplication) redact(ctx context.Context, handle *WorkspaceHandle, value string) string {
	return application.ports.Redact(ctx, handle, value)
}
