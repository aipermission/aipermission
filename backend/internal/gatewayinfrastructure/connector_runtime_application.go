package gatewayinfrastructure

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

type ConnectorRuntimeExecutionPorts struct {
	Principal func(*WorkspaceHandle) (gatewayaccess.Principal, error)
	Restart   func(context.Context, *WorkspaceHandle, gatewayaccess.Principal, int64, string) (connectorapi.ConsoleRestartResult, error)
}

type ConnectorRuntimeTransferPorts struct {
	Download func(context.Context, *WorkspaceHandle, connectorapi.TransferAuthorization, int64, []string, string, string) (connectorapi.TransferBatch, error)
}

type ConnectorRuntimeObservationPorts struct {
	Audit func(context.Context, *WorkspaceHandle, string, *int64, int64, string, any)
}

type ConnectorActionFinishPort func(context.Context, *WorkspaceHandle, int64, connectors.ResultStatus, any, string, string, ...connectors.OutputHint) (connectormgmt.ActionRequest, error)

type ConnectorTargetWorkflowPorts struct {
	Delete   func(context.Context, connectormgmt.Target, map[string]any) error
	Finalize func(context.Context, connectormgmt.Target, string, map[string]any) (int64, error)
}

type ConnectorRuntimeDependencies struct {
	TrustStorePath      func() string
	ActiveRuntime       func(http.ResponseWriter) bool
	WorkspaceSnapshot   func() []*WorkspaceHandle
	InvalidatePeerTrust func(context.Context, *WorkspaceHandle, string) error
	Execution           ConnectorRuntimeExecutionPorts
	Transfers           ConnectorRuntimeTransferPorts
	Observation         ConnectorRuntimeObservationPorts
}

// ConnectorRuntimeApplication owns adapter selection and runtime capability
// composition. HTTP/MCP transports call this application without inspecting a
// connector implementation or constructing a connector workspace.
type ConnectorRuntimeApplication struct {
	owner       *ConnectorPortsOwner
	adapters    connectorapi.Catalog
	ports       *connectorports.PortsComponent
	execution   ConnectorRuntimeExecutionPorts
	transfers   ConnectorRuntimeTransferPorts
	observation ConnectorRuntimeObservationPorts
	trust       func() string
}

func NewConnectorRuntimeApplication(owner *ConnectorPortsOwner, operations *OperationsOwner, adapters connectorapi.Catalog, dependencies ConnectorRuntimeDependencies) (*ConnectorRuntimeApplication, error) {
	if owner == nil || operations == nil {
		return nil, errors.New("connector runtime owners are required")
	}
	if adapters == nil {
		return nil, errors.New("connector adapter registry is required")
	}
	if dependencies.TrustStorePath == nil || dependencies.ActiveRuntime == nil ||
		dependencies.WorkspaceSnapshot == nil || dependencies.InvalidatePeerTrust == nil ||
		dependencies.Execution.Principal == nil || dependencies.Execution.Restart == nil ||
		dependencies.Transfers.Download == nil || dependencies.Observation.Audit == nil {
		return nil, errors.New("connector runtime ports are incomplete")
	}
	application := &ConnectorRuntimeApplication{
		owner: owner, adapters: adapters, execution: dependencies.Execution,
		transfers: dependencies.Transfers, observation: dependencies.Observation,
		trust: dependencies.TrustStorePath,
	}
	peerTrust := connectorports.NewPeerTrustCoordinator(func() []connectorports.PeerTrustWorkspace {
		handles := dependencies.WorkspaceSnapshot()
		workspaces := make([]connectorports.PeerTrustWorkspace, 0, len(handles))
		for _, handle := range handles {
			boundHandle := handle
			workspace, ok := operations.PeerTrustWorkspace(handle, func(ctx context.Context, reason string) error {
				return dependencies.InvalidatePeerTrust(ctx, boundHandle, reason)
			})
			if ok {
				workspaces = append(workspaces, workspace)
			}
		}
		return workspaces
	})
	application.ports = connectorports.NewPorts(connectorports.PortsDependencies{
		Peer: connectorports.PeerDependencies{TrustStorePath: dependencies.TrustStorePath},
		Routes: connectorports.RouteDependencies{
			ActiveRuntime: dependencies.ActiveRuntime,
			PeerTrust:     peerTrust,
		},
		LiveConsole: connectorports.LiveConsoleDependencies{
			TransportAdapter: application.liveConsoleTransportAdapter,
			TargetAdapter:    application.liveConsoleTargetAdapter,
		},
	})
	return application, nil
}

func (application *ConnectorRuntimeApplication) workspace(handle *WorkspaceHandle, full bool, finish ConnectorActionFinishPort, targets ConnectorTargetWorkflowPorts) (connectorports.Workspace, bool) {
	if application == nil || handle == nil {
		return connectorports.Workspace{}, false
	}
	bindings := connectorports.Workspace{}
	if application.execution.Principal != nil {
		bindings.Principal = func() (gatewayaccess.Principal, error) {
			return application.execution.Principal(handle)
		}
	}
	if full {
		bindings.Actions = connectorports.WorkspaceActionPorts{
			Restart: func(ctx context.Context, principal gatewayaccess.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
				if application.execution.Restart == nil {
					return connectorapi.ConsoleRestartResult{}, errors.New("connector restart port is unavailable")
				}
				return application.execution.Restart(ctx, handle, principal, runtimeID, runningError)
			},
			Finish: connectormgmt.DomainActionFinish(func(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
				if finish == nil {
					return connectormgmt.ActionRequest{}, errors.New("connector finish port is unavailable")
				}
				return finish(ctx, handle, requestID, status, output, displayText, errorText, hints...)
			}),
		}
		bindings.Transfers = connectorports.WorkspaceTransferPorts{
			RunDownloadBatch: func(ctx context.Context, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
				if application.transfers.Download == nil {
					return connectorapi.TransferBatch{}, errors.New("connector download port is unavailable")
				}
				return application.transfers.Download(ctx, handle, authorization, runtimeID, paths, archiveName, source)
			},
			RuntimeCapabilities: func(kind string) connectors.RuntimeCapabilityResolver {
				return application.RuntimeCapabilities(handle, kind)
			},
		}
		bindings.Targets = connectorports.WorkspaceTargetPorts{
			Delete: connectormgmt.DomainTargetDelete(func(ctx context.Context, target connectormgmt.Target, payload map[string]any) error {
				if targets.Delete == nil {
					return errors.New("connector delete port is unavailable")
				}
				return targets.Delete(ctx, target, payload)
			}),
			Finalize: connectormgmt.DomainTargetFinalize(func(ctx context.Context, target connectormgmt.Target, reason string, payload map[string]any) (int64, error) {
				if targets.Finalize == nil {
					return 0, errors.New("connector finalize port is unavailable")
				}
				return targets.Finalize(ctx, target, reason, payload)
			}),
			Audit: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
				if application.observation.Audit != nil {
					application.observation.Audit(ctx, handle, actor, tokenID, runtimeID, action, payload)
				}
			},
		}
	}
	return application.owner.connectorWorkspace(handle, bindings)
}

type runtimeCapabilities map[string]connectors.RuntimeCapability

func (capabilities runtimeCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	return capabilities[name]
}

func (application *ConnectorRuntimeApplication) RuntimeCapabilities(handle *WorkspaceHandle, kind string) connectors.RuntimeCapabilityResolver {
	return application.runtimeCapabilities(handle, kind, nil, false, nil)
}

func (application *ConnectorRuntimeApplication) ActionCapabilities(handle *WorkspaceHandle, kind string, dependencies []connectors.ResolvedDependency, finish ConnectorActionFinishPort) connectors.RuntimeCapabilityResolver {
	return application.runtimeCapabilities(handle, kind, dependencies, true, finish)
}

func (application *ConnectorRuntimeApplication) runtimeCapabilities(handle *WorkspaceHandle, kind string, dependencies []connectors.ResolvedDependency, approved bool, finish ConnectorActionFinishPort) connectors.RuntimeCapabilityResolver {
	capabilities := runtimeCapabilities{}
	workspace, ok := application.workspace(handle, true, finish, ConnectorTargetWorkflowPorts{})
	if !ok {
		return nil
	}
	adapterFor := application.adapters.For
	if approved {
		capabilities[connectors.NetworkTransportCapabilityName] = connectorports.ApprovedNetworkTransport(workspace, adapterFor, application.trust, dependencies)
		capabilities[connectors.CommandTransportCapabilityName] = connectorports.ApprovedCommandTransport(workspace, adapterFor, application.trust, dependencies)
	} else {
		network := connectorports.NetworkTransport(workspace, adapterFor, application.trust)
		capabilities[network.ConnectorRuntimeCapability()] = network
		command := connectorports.CommandTransport(workspace, adapterFor, application.trust)
		capabilities[command.ConnectorRuntimeCapability()] = command
	}
	adapter, _ := application.adapters.For(kind).(connectorapi.RuntimeAdapter)
	if adapter != nil {
		gateway, runtime := application.ports.RuntimeActionPorts(workspace, kind)
		var err error
		capabilities, err = mergeRuntimeCapabilities(capabilities, adapter.RuntimeCapabilities(gateway, runtime))
		if err != nil {
			log.Printf("connector runtime capabilities rejected kind=%s error=%v", kind, err)
			return nil
		}
	}
	if len(capabilities) == 0 {
		return nil
	}
	return capabilities
}

func mergeRuntimeCapabilities(base runtimeCapabilities, additions map[string]connectors.RuntimeCapability) (runtimeCapabilities, error) {
	merged := make(runtimeCapabilities, len(base)+len(additions))
	for name, capability := range base {
		merged[name] = capability
	}
	for name, capability := range additions {
		if !connectors.ValidIdentifier(name) {
			return nil, fmt.Errorf("invalid runtime capability name %q", name)
		}
		if capability == nil {
			return nil, fmt.Errorf("runtime capability %q is nil", name)
		}
		if declared := capability.ConnectorRuntimeCapability(); declared != name {
			return nil, fmt.Errorf("runtime capability %q declares name %q", name, declared)
		}
		if _, exists := merged[name]; exists {
			return nil, fmt.Errorf("runtime capability %q collides with a protected capability", name)
		}
		merged[name] = capability
	}
	return merged, nil
}

func (application *ConnectorRuntimeApplication) RunningHint(request connectormgmt.ActionRequest) string {
	adapter, _ := application.adapters.For(request.ConnectorKind).(connectorapi.RuntimeAdapter)
	if adapter == nil {
		return ""
	}
	return strings.TrimSpace(adapter.RunningHint(connectorActionRequest(request)))
}

func (application *ConnectorRuntimeApplication) RunningHintPort() gatewayaccess.MCPRunningHint {
	return connectormgmt.DomainRunningHint(application.RunningHint)
}

func (application *ConnectorRuntimeApplication) SupportsRunning(prepared gatewayactions.PreparedRequest) bool {
	adapterPrepared := prepared.Adapter()
	adapter, _ := application.adapters.For(adapterPrepared.TargetConnectorKind).(connectorapi.RuntimeAdapter)
	return adapter != nil && adapter.SupportsRunning(adapterPrepared)
}

func (application *ConnectorRuntimeApplication) FinishRunning(ctx context.Context, handle *WorkspaceHandle, requestID int64, prepared gatewayactions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles, finish ConnectorActionFinishPort) {
	adapterPrepared := prepared.Adapter()
	adapter, _ := application.adapters.For(adapterPrepared.TargetConnectorKind).(connectorapi.RuntimeAdapter)
	if adapter == nil || !adapter.SupportsRunning(adapterPrepared) {
		return
	}
	workspace, ok := application.workspace(handle, true, finish, ConnectorTargetWorkflowPorts{})
	if !ok {
		return
	}
	gateway, runtime := application.ports.ActionFinishPorts(workspace, adapterPrepared.TargetConnectorKind)
	if err := adapter.FinishRunning(ctx, gateway, runtime, requestID, adapterPrepared, connectorPrincipal(principal), handles); err != nil {
		log.Printf("finish running connector action failed connector=%q request=%d error=%v", adapterPrepared.TargetConnectorKind, requestID, err)
	}
}

func (application *ConnectorRuntimeApplication) DataRuntime(handle *WorkspaceHandle, kind string) connectorapi.ConnectorDataRuntime {
	workspace, _ := application.workspace(handle, false, nil, ConnectorTargetWorkflowPorts{})
	return connectorports.DataRuntime(workspace, kind)
}

func (application *ConnectorRuntimeApplication) LiveRuntime(handle *WorkspaceHandle, kind string) connectorapi.LiveConsoleRuntime {
	workspace, _ := application.workspace(handle, false, nil, ConnectorTargetWorkflowPorts{})
	return connectorports.LiveRuntime(workspace, kind)
}

func (application *ConnectorRuntimeApplication) CredentialResourceRuntime(handle *WorkspaceHandle, kind string) connectorapi.CredentialResourceRuntime {
	workspace, _ := application.workspace(handle, false, nil, ConnectorTargetWorkflowPorts{})
	return connectorports.PortCredentialResourceRuntime(workspace, kind)
}

func (application *ConnectorRuntimeApplication) TargetLifecycleRuntime(handle *WorkspaceHandle, kind string) connectorapi.TargetLifecycleRuntime {
	workspace, _ := application.workspace(handle, false, nil, ConnectorTargetWorkflowPorts{})
	return application.ports.TargetLifecycleRuntime(workspace, kind)
}

func (application *ConnectorRuntimeApplication) PeerGateway() connectorapi.PeerIdentityGateway {
	return application.ports.PeerGateway()
}

func (application *ConnectorRuntimeApplication) ReadRouteGateway() connectorapi.ReadRouteGateway {
	return application.ports.RouteGateway()
}

func (application *ConnectorRuntimeApplication) MutationRouteGateway() connectorapi.MutationRouteGateway {
	return application.ports.RouteGateway()
}

func (application *ConnectorRuntimeApplication) LiveConsoleGateway(handle *WorkspaceHandle) connectorapi.LiveConsoleGateway {
	workspace, _ := application.workspace(handle, true, nil, ConnectorTargetWorkflowPorts{})
	return application.ports.LiveConsoleGateway(workspace)
}

func (application *ConnectorRuntimeApplication) RuntimeActionPorts(handle *WorkspaceHandle, kind string) (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
	workspace, _ := application.workspace(handle, true, nil, ConnectorTargetWorkflowPorts{})
	return application.ports.RuntimeActionPorts(workspace, kind)
}

func (application *ConnectorRuntimeApplication) TargetDeletionGateway(handle *WorkspaceHandle, targets ConnectorTargetWorkflowPorts) func(string, int64) connectorapi.TargetDeletionGateway {
	workspace, _ := application.workspace(handle, true, nil, targets)
	return application.ports.TargetDeletionGatewayProvider(workspace)
}

func (application *ConnectorRuntimeApplication) TargetOperationGateway(handle *WorkspaceHandle) func(string, int64) connectorapi.TargetOperationGateway {
	workspace, _ := application.workspace(handle, true, nil, ConnectorTargetWorkflowPorts{})
	return application.ports.TargetOperationGatewayProvider(workspace)
}

func (application *ConnectorRuntimeApplication) NetworkProbe(ctx context.Context, handle *WorkspaceHandle, request connectors.NetworkDialRequest) error {
	workspace, ok := application.workspace(handle, false, nil, ConnectorTargetWorkflowPorts{})
	if !ok {
		return errors.New("connector runtime is unavailable")
	}
	transport := connectorports.NetworkTransport(workspace, application.adapters.For, application.trust)
	connection, err := transport.DialConnectorTCP(ctx, request)
	if err != nil {
		return err
	}
	if connection == nil {
		return errors.New("connector transport returned no connection")
	}
	_ = connection.Close()
	return nil
}

func (application *ConnectorRuntimeApplication) LiveConsoleCapabilityKind(kind string) (string, bool) {
	adapter := application.liveConsoleTargetAdapter(kind)
	if adapter == nil {
		return "", false
	}
	return adapter.LiveConsoleCapabilityKind(), true
}

func (application *ConnectorRuntimeApplication) MetadataAdapter(kind string) gatewayaccess.MCPMetadataAdapter {
	return application.liveConsoleTargetAdapter(kind)
}

func (application *ConnectorRuntimeApplication) LiveConsoleActionName(kind string) (string, bool) {
	adapter, ok := application.adapters.For(kind).(connectorapi.LiveConsoleAdapter)
	if !ok || strings.TrimSpace(adapter.LiveConsoleActionName()) == "" {
		return "", false
	}
	return strings.TrimSpace(adapter.LiveConsoleActionName()), true
}

func (application *ConnectorRuntimeApplication) LiveConsoleTargetMetadata(kind string, target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	adapter := application.liveConsoleTargetAdapter(kind)
	if adapter == nil {
		return nil
	}
	return adapter.LiveConsoleTargetMetadata(target, profile)
}

func (application *ConnectorRuntimeApplication) ExpectedLiveConsolePeerIdentities(ctx context.Context, handle *WorkspaceHandle, kind string, runtimeID int64) ([]string, bool, error) {
	adapter, ok := application.adapters.For(kind).(connectorapi.LiveConsolePeerIdentityAdapter)
	if !ok {
		return nil, false, nil
	}
	items, err := adapter.ExpectedLiveConsolePeerIdentities(ctx, application.PeerGateway(), application.LiveRuntime(handle, kind), runtimeID)
	return items, true, err
}

func (application *ConnectorRuntimeApplication) ErrorPresenter(kind string) connectorapi.ErrorPresenter {
	adapter, _ := application.adapters.For(kind).(connectorapi.ErrorPresenter)
	return adapter
}

func (application *ConnectorRuntimeApplication) HasFileTransfer(kind string) bool {
	adapter, _ := application.adapters.For(kind).(connectorapi.FileTransferAdapter)
	return adapter != nil
}

func (application *ConnectorRuntimeApplication) HasTCPTransport(kind string) bool {
	adapter, _ := application.adapters.For(kind).(connectorapi.TCPTransportAdapter)
	return adapter != nil
}

func (application *ConnectorRuntimeApplication) liveConsoleTargetAdapter(kind string) connectorapi.LiveConsoleTargetAdapter {
	adapter, _ := application.adapters.For(kind).(connectorapi.LiveConsoleTargetAdapter)
	return adapter
}

func (application *ConnectorRuntimeApplication) liveConsoleTransportAdapter(kind string) connectorapi.LiveConsoleTransportAdapter {
	adapter, _ := application.adapters.For(kind).(connectorapi.LiveConsoleTransportAdapter)
	return adapter
}

func (application *ConnectorRuntimeApplication) ResolveLiveConsoleTarget(ctx context.Context, handle *WorkspaceHandle, kinds []string, runtimeID int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, kind := range kinds {
		adapter := application.liveConsoleTargetAdapter(kind)
		if adapter == nil {
			continue
		}
		ref, err := adapter.LiveConsoleTargetRef(ctx, application.LiveRuntime(handle, kind), runtimeID)
		if connectormgmt.IsRuntimeSurfaceNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("resolve %s live console runtime: %w", kind, err)
		}
		if ref == "" {
			return "", fmt.Errorf("resolve %s live console runtime: empty target reference", kind)
		}
		return ref, nil
	}
	return "", connectormgmt.InvalidTargetRefError()
}

func (application *ConnectorRuntimeApplication) OpenLiveConsole(ctx context.Context, handle *WorkspaceHandle, kind string, request connectorapi.LiveConsoleOpenRequest) (*connectorapi.LiveConsoleSession, error) {
	adapter := application.liveConsoleTransportAdapter(kind)
	if adapter == nil {
		return nil, connectormgmt.InvalidTargetRefError()
	}
	session, err := adapter.OpenLiveConsole(ctx, application.LiveConsoleGateway(handle), application.LiveRuntime(handle, kind), request)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("connector live console returned no session")
	}
	return session, nil
}

func (application *ConnectorRuntimeApplication) CredentialProfileLifecycleAdapter(kind string) connectorapi.CredentialProfileLifecycleAdapter {
	adapter, _ := application.adapters.For(kind).(connectorapi.CredentialProfileLifecycleAdapter)
	return adapter
}

func (application *ConnectorRuntimeApplication) CredentialProfileTester(kind string) connectorapi.CredentialProfileTester {
	adapter, _ := application.adapters.For(kind).(connectorapi.CredentialProfileTester)
	return adapter
}

func (application *ConnectorRuntimeApplication) CredentialCanonicalizer(kind string) connectorapi.CredentialCanonicalizer {
	adapter, _ := application.adapters.For(kind).(connectorapi.CredentialCanonicalizer)
	return adapter
}

func (application *ConnectorRuntimeApplication) FileTransferAdapter(kind string) connectorapi.FileTransferAdapter {
	adapter, _ := application.adapters.For(kind).(connectorapi.FileTransferAdapter)
	return adapter
}

func (application *ConnectorRuntimeApplication) CredentialResourceAdapter(kind string) connectorapi.CredentialResourceAdapter {
	adapter, _ := application.adapters.For(kind).(connectorapi.CredentialResourceAdapter)
	return adapter
}

func (application *ConnectorRuntimeApplication) RouteDefinitions(kinds []string) ([]connectorapi.RouteDefinition, error) {
	return application.adapters.RouteDefinitions(kinds)
}
