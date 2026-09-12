// Package connectorports binds connector contracts to workspace-scoped
// gateway capabilities without re-exporting connector-owned types.
package connectorports

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

var ErrRuntimeUnavailable = errors.New("connector runtime is unavailable")

type PeerDependencies struct {
	TrustStorePath func() string
}

type RouteDependencies struct {
	ActiveRuntime func(http.ResponseWriter) bool
	PeerTrust     *PeerTrustCoordinator
}

type LiveConsoleDependencies struct {
	TransportAdapter func(string) connectorapi.LiveConsoleTransportAdapter
	TargetAdapter    func(string) connectorapi.LiveConsoleTargetAdapter
}

type Workspace struct {
	runtime   connectortransport.Runtime
	Principal func() (executionprincipal.Principal, error)
	Actions   WorkspaceActionPorts
	Transfers WorkspaceTransferPorts
	Targets   WorkspaceTargetPorts
}

type WorkspaceActionPorts struct {
	Restart func(context.Context, executionprincipal.Principal, int64, string) (connectorapi.ConsoleRestartResult, error)
	Finish  func(context.Context, int64, connectors.ResultStatus, any, string, string, ...connectors.OutputHint) (connectortargets.ActionRequest, error)
}

type WorkspaceTransferPorts struct {
	RunDownloadBatch    func(context.Context, connectorapi.TransferAuthorization, int64, []string, string, string) (connectorapi.TransferBatch, error)
	RuntimeCapabilities func(string) connectors.RuntimeCapabilityResolver
}

type WorkspaceTargetPorts struct {
	Delete   func(context.Context, connectortargets.Target, map[string]any) error
	Finalize func(context.Context, connectortargets.Target, string, map[string]any) (int64, error)
	Audit    func(context.Context, string, *int64, int64, string, any)
}

type PortsDependencies struct {
	Peer        PeerDependencies
	Routes      RouteDependencies
	LiveConsole LiveConsoleDependencies
}

type PortsComponent struct{ dependencies PortsDependencies }

func NewPorts(dependencies PortsDependencies) *PortsComponent {
	return &PortsComponent{dependencies: dependencies}
}

func NewWorkspace(scopes connectortransport.ScopeRuntime, database *sql.DB, acquireDelivery func(context.Context) (func(), error)) Workspace {
	return Workspace{runtime: connectortransport.Runtime{Scopes: scopes, Database: database, AcquireDelivery: acquireDelivery}}
}

func (workspace Workspace) WithPrincipal(principal func() (executionprincipal.Principal, error)) Workspace {
	workspace.Principal = principal
	return workspace
}

func DataRuntime(workspace Workspace, kind string) connectorapi.ConnectorDataRuntime {
	return connectortransport.DataRuntime(workspace.runtime, kind)
}

func LiveRuntime(workspace Workspace, kind string) connectorapi.LiveConsoleRuntime {
	return connectortransport.LiveRuntime(workspace.runtime, kind)
}

func PortActionRuntime(workspace Workspace, kind string) connectorapi.ActionRuntime {
	return connectortransport.ActionRuntime(workspace.runtime, kind)
}

func PortCredentialResourceRuntime(workspace Workspace, kind string) connectorapi.CredentialResourceRuntime {
	return connectortransport.CredentialResourceRuntime(workspace.runtime, kind)
}

type SecretAccessorFactory func(map[string]any) connectors.SecretAccessor

func TransferRuntimeWithSecretAccessor(workspace Workspace, kind string, accessor SecretAccessorFactory) connectorapi.TransferRuntime {
	scope := connectortransport.ScopeWithSecretAccessor(workspace.runtime, kind, connectorruntime.SecretAccessorFactory(accessor))
	return scope.TransferRuntime()
}

func (component *PortsComponent) TargetLifecycleRuntime(workspace Workspace, kind string) connectorapi.TargetLifecycleRuntime {
	return connectortransport.TargetLifecycleRuntime(workspace.runtime, kind, workspace.Principal)
}

type PeerGateway struct{ component *PortsComponent }

func (gateway PeerGateway) ConnectorTrustStorePath() string {
	if gateway.component == nil || gateway.component.dependencies.Peer.TrustStorePath == nil {
		return ""
	}
	return gateway.component.dependencies.Peer.TrustStorePath()
}

type RouteGateway struct{ PeerGateway }

func (gateway RouteGateway) ConnectorActiveRuntimeAvailable(w http.ResponseWriter) bool {
	return gateway.component != nil && gateway.component.dependencies.Routes.ActiveRuntime != nil && gateway.component.dependencies.Routes.ActiveRuntime(w)
}

func (gateway RouteGateway) ConnectorChangeVaultPeerTrust(ctx context.Context, change func() error) error {
	if gateway.component == nil || gateway.component.dependencies.Routes.PeerTrust == nil {
		return ErrPeerTrustUnavailable
	}
	return gateway.component.dependencies.Routes.PeerTrust.Change(ctx, change)
}

func (component *PortsComponent) PeerGateway() PeerGateway { return PeerGateway{component: component} }
func (component *PortsComponent) RouteGateway() RouteGateway {
	return RouteGateway{PeerGateway: component.PeerGateway()}
}

type LiveConsoleGateway struct {
	PeerGateway
	workspace Workspace
}

func (component *PortsComponent) LiveConsoleGateway(workspace Workspace) LiveConsoleGateway {
	return LiveConsoleGateway{PeerGateway: component.PeerGateway(), workspace: workspace}
}

func (gateway LiveConsoleGateway) ConnectorOpenLiveConsole(ctx context.Context, targetRef string, rows, cols int, params map[string]any) (*connectorapi.LiveConsoleSession, error) {
	if gateway.component == nil || gateway.workspace.runtime.Database == nil || gateway.workspace.runtime.Scopes == nil ||
		gateway.component.dependencies.LiveConsole.TransportAdapter == nil || gateway.component.dependencies.LiveConsole.TargetAdapter == nil {
		return nil, ErrRuntimeUnavailable
	}
	store := connectortargets.NewStore(gateway.workspace.runtime.Database)
	target, profile, err := store.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return nil, err
	}
	transport := gateway.component.dependencies.LiveConsole.TransportAdapter(target.ConnectorKind)
	targetAdapter := gateway.component.dependencies.LiveConsole.TargetAdapter(target.ConnectorKind)
	if transport == nil || targetAdapter == nil {
		return nil, connectortargets.ErrInvalidTargetRef
	}
	surface, err := store.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: target.ConnectorKind, TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: targetAdapter.LiveConsoleCapabilityKind(), Label: profile.Label,
	})
	if err != nil {
		return nil, err
	}
	return transport.OpenLiveConsole(ctx, gateway, LiveRuntime(gateway.workspace, target.ConnectorKind), connectorapi.LiveConsoleOpenRequest{RuntimeID: surface.ID, Rows: rows, Cols: cols, Params: params})
}

type RuntimeActionGateway struct {
	PeerGateway
	workspace Workspace
	kind      string
}

func (component *PortsComponent) RuntimeActionPorts(workspace Workspace, kind string) (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
	return RuntimeActionGateway{PeerGateway: component.PeerGateway(), workspace: workspace, kind: kind}, PortActionRuntime(workspace, kind)
}

func (gateway RuntimeActionGateway) ConnectorRestartConsoleSession(ctx context.Context, principal connectorapi.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
	if gateway.workspace.Actions.Restart == nil {
		return connectorapi.ConsoleRestartResult{}, ErrRuntimeUnavailable
	}
	if err := connectortransport.RequireRuntimeID(ctx, gateway.workspace.runtime, gateway.kind, runtimeID); err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	core, err := corePrincipal(principal)
	if err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	return gateway.workspace.Actions.Restart(ctx, core, runtimeID, runningError)
}

func (gateway RuntimeActionGateway) ConnectorCreateAndRunDownloadBatch(ctx context.Context, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
	if gateway.workspace.Transfers.RunDownloadBatch == nil {
		return connectorapi.TransferBatch{}, ErrRuntimeUnavailable
	}
	if err := connectortransport.RequireRuntimeID(ctx, gateway.workspace.runtime, gateway.kind, runtimeID); err != nil {
		return connectorapi.TransferBatch{}, err
	}
	return gateway.workspace.Transfers.RunDownloadBatch(ctx, authorization, runtimeID, paths, archiveName, source)
}

type ActionFinishGateway struct {
	component *PortsComponent
	workspace Workspace
	kind      string
}

func (component *PortsComponent) ActionFinishPorts(workspace Workspace, kind string) (connectorapi.ActionFinishGateway, connectorapi.ActionRuntime) {
	return ActionFinishGateway{component: component, workspace: workspace, kind: kind}, PortActionRuntime(workspace, kind)
}

func (gateway ActionFinishGateway) ConnectorFinishActionRequest(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectorapi.ActionRequest, error) {
	if gateway.workspace.runtime.Database == nil || gateway.workspace.Actions.Finish == nil {
		return connectorapi.ActionRequest{}, ErrRuntimeUnavailable
	}
	request, err := connectortargets.NewStore(gateway.workspace.runtime.Database).GetActionRequest(ctx, requestID)
	if err != nil {
		return connectorapi.ActionRequest{}, err
	}
	if request.ConnectorKind != gateway.kind {
		return connectorapi.ActionRequest{}, connectortargets.ErrActionRequestNotFound
	}
	finished, err := gateway.workspace.Actions.Finish(ctx, requestID, status, output, displayText, errorText, hints...)
	return gatewayActionRequest(finished), err
}

type FileTransferGateway struct {
	PeerGateway
	workspace Workspace
	kind      string
}

func (component *PortsComponent) FileTransferGateway(workspace Workspace, kind string) connectorapi.FileTransferGateway {
	return FileTransferGateway{PeerGateway: component.PeerGateway(), workspace: workspace, kind: kind}
}

func (gateway FileTransferGateway) ConnectorRuntimeCapabilities() connectors.RuntimeCapabilityResolver {
	if gateway.workspace.Transfers.RuntimeCapabilities == nil {
		return nil
	}
	return gateway.workspace.Transfers.RuntimeCapabilities(gateway.kind)
}

type TargetDeletionGateway struct {
	PeerGateway
	workspace Workspace
	kind      string
	targetID  int64
}

func (component *PortsComponent) TargetDeletionGateway(workspace Workspace, kind string, targetID int64) connectorapi.TargetDeletionGateway {
	return TargetDeletionGateway{PeerGateway: component.PeerGateway(), workspace: workspace, kind: kind, targetID: targetID}
}

func (component *PortsComponent) TargetDeletionGatewayProvider(workspace Workspace) func(string, int64) connectorapi.TargetDeletionGateway {
	return func(kind string, targetID int64) connectorapi.TargetDeletionGateway {
		if component == nil {
			return nil
		}
		return component.TargetDeletionGateway(workspace, kind, targetID)
	}
}

func (gateway TargetDeletionGateway) ConnectorRestartConsoleSession(ctx context.Context, principal connectorapi.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
	if gateway.workspace.Actions.Restart == nil {
		return connectorapi.ConsoleRestartResult{}, ErrRuntimeUnavailable
	}
	if err := connectortransport.RequireRuntimeID(ctx, gateway.workspace.runtime, gateway.kind, runtimeID); err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	core, err := corePrincipal(principal)
	if err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	return gateway.workspace.Actions.Restart(ctx, core, runtimeID, runningError)
}

func (gateway TargetDeletionGateway) ConnectorDeleteTargetRecord(ctx context.Context, target connectorapi.Target, payload map[string]any) error {
	if target.ID != gateway.targetID || target.ConnectorKind != gateway.kind {
		return connectortargets.ErrTargetNotFound
	}
	if gateway.workspace.Targets.Delete == nil {
		return ErrRuntimeUnavailable
	}
	return gateway.workspace.Targets.Delete(ctx, coreTarget(target), payload)
}

func (gateway TargetDeletionGateway) ConnectorFinalizeDeletedTarget(ctx context.Context, target connectorapi.Target, reason string, payload map[string]any) (int64, error) {
	if target.ID != gateway.targetID || target.ConnectorKind != gateway.kind {
		return 0, connectortargets.ErrTargetNotFound
	}
	if gateway.workspace.Targets.Finalize == nil {
		return 0, ErrRuntimeUnavailable
	}
	return gateway.workspace.Targets.Finalize(ctx, coreTarget(target), reason, payload)
}

type TargetOperationGateway struct {
	PeerGateway
	workspace Workspace
	kind      string
	targetID  int64
}

func (component *PortsComponent) TargetOperationGateway(workspace Workspace, kind string, targetID int64) connectorapi.TargetOperationGateway {
	return TargetOperationGateway{PeerGateway: component.PeerGateway(), workspace: workspace, kind: kind, targetID: targetID}
}

func (component *PortsComponent) TargetOperationGatewayProvider(workspace Workspace) func(string, int64) connectorapi.TargetOperationGateway {
	return func(kind string, targetID int64) connectorapi.TargetOperationGateway {
		if component == nil {
			return nil
		}
		return component.TargetOperationGateway(workspace, kind, targetID)
	}
}

func (gateway TargetOperationGateway) ConnectorWriteAudit(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	if gateway.workspace.Targets.Audit != nil && connectortransport.RequireTargetRuntimeID(ctx, gateway.workspace.runtime, gateway.kind, gateway.targetID, runtimeID) == nil {
		gateway.workspace.Targets.Audit(ctx, actor, tokenID, runtimeID, action, payload)
	}
}

func RequireRuntimeID(ctx context.Context, workspace Workspace, kind string, runtimeID int64) error {
	return connectortransport.RequireRuntimeID(ctx, workspace.runtime, kind, runtimeID)
}

func RequireTargetRuntimeID(ctx context.Context, workspace Workspace, kind string, targetID, runtimeID int64) error {
	return connectortransport.RequireTargetRuntimeID(ctx, workspace.runtime, kind, targetID, runtimeID)
}

var (
	_ connectorapi.RouteGateway           = RouteGateway{}
	_ connectorapi.LiveConsoleGateway     = LiveConsoleGateway{}
	_ connectorapi.RuntimeActionGateway   = RuntimeActionGateway{}
	_ connectorapi.ActionFinishGateway    = ActionFinishGateway{}
	_ connectorapi.FileTransferGateway    = FileTransferGateway{}
	_ connectorapi.TargetDeletionGateway  = TargetDeletionGateway{}
	_ connectorapi.TargetOperationGateway = TargetOperationGateway{}
)
