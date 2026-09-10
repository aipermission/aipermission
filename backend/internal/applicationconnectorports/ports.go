// Package applicationconnectorports owns the gateway ports exposed to connector adapters.
package applicationconnectorports

import (
	"context"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

var ErrRuntimeUnavailable = errors.New("connector runtime is unavailable")

type Dependencies struct {
	TrustStorePath        func() string
	ActiveRuntime         func(http.ResponseWriter) bool
	ChangePeerTrust       func(context.Context, func() error) error
	LocalPrincipal        func(*workspaceruntime.Runtime) (executionprincipal.Principal, error)
	LiveTransportAdapter  func(string) connectorapi.LiveConsoleTransportAdapter
	LiveTargetAdapter     func(string) connectorapi.LiveConsoleTargetAdapter
	RestartConsole        func(context.Context, *workspaceruntime.Runtime, executionprincipal.Principal, int64, string) (connectorapi.ConsoleRestartResult, error)
	RunDownloadBatch      func(context.Context, *workspaceruntime.Runtime, connectorapi.TransferAuthorization, int64, []string, string, string) (connectorapi.TransferBatch, error)
	FinishAction          func(context.Context, *workspaceruntime.Runtime, int64, connectors.ResultStatus, any, string, string, ...connectors.OutputHint) (connectortargets.ActionRequest, error)
	RuntimeCapabilities   func(string, *workspaceruntime.Runtime) connectors.RuntimeCapabilityResolver
	DeleteTarget          func(context.Context, *workspaceruntime.Runtime, connectortargets.Target, map[string]any) error
	FinalizeDeletedTarget func(context.Context, *workspaceruntime.Runtime, connectortargets.Target, string, map[string]any) (int64, error)
	WriteAudit            func(context.Context, *workspaceruntime.Runtime, string, *int64, int64, string, any)
}

type Component struct{ dependencies Dependencies }

type TargetLifecycleRuntimePort = connectortransport.TargetLifecycleRuntimePort

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

func DataRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.ConnectorDataRuntime {
	return connectortransport.DataRuntime(runtime, kind)
}

func LiveRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.LiveConsoleRuntime {
	return connectortransport.LiveRuntime(runtime, kind)
}

func ActionRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.ActionRuntime {
	return connectortransport.ActionRuntime(runtime, kind)
}

func CredentialResourceRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.CredentialResourceRuntime {
	return connectortransport.CredentialResourceRuntime(runtime, kind)
}

func (component *Component) TargetLifecycleRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.TargetLifecycleRuntime {
	return connectortransport.TargetLifecycleRuntime(runtime, kind, func() (executionprincipal.Principal, error) {
		return component.dependencies.LocalPrincipal(runtime)
	})
}

type PeerGateway struct{ component *Component }

func (gateway PeerGateway) ConnectorTrustStorePath() string {
	if gateway.component == nil || gateway.component.dependencies.TrustStorePath == nil {
		return ""
	}
	return gateway.component.dependencies.TrustStorePath()
}

type RouteGateway struct{ PeerGateway }

func (gateway RouteGateway) ConnectorActiveRuntimeAvailable(w http.ResponseWriter) bool {
	return gateway.component != nil && gateway.component.dependencies.ActiveRuntime(w)
}

func (gateway RouteGateway) ConnectorChangeVaultPeerTrust(ctx context.Context, change func() error) error {
	return gateway.component.dependencies.ChangePeerTrust(ctx, change)
}

func (component *Component) PeerGateway() PeerGateway { return PeerGateway{component: component} }
func (component *Component) RouteGateway() RouteGateway {
	return RouteGateway{PeerGateway: component.PeerGateway()}
}

type LiveConsoleGateway struct {
	PeerGateway
	runtime *workspaceruntime.Runtime
}

func (component *Component) LiveConsoleGateway(runtime *workspaceruntime.Runtime) LiveConsoleGateway {
	return LiveConsoleGateway{PeerGateway: component.PeerGateway(), runtime: runtime}
}

func (gateway LiveConsoleGateway) ConnectorOpenLiveConsole(ctx context.Context, targetRef string, rows, cols int, params map[string]any) (*console.RuntimeSession, error) {
	if gateway.component == nil || gateway.runtime == nil || gateway.runtime.Storage.Database == nil {
		return nil, ErrRuntimeUnavailable
	}
	store := connectortargets.NewStore(gateway.runtime.Storage.Database)
	target, profile, err := store.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return nil, err
	}
	transport := gateway.component.dependencies.LiveTransportAdapter(target.ConnectorKind)
	targetAdapter := gateway.component.dependencies.LiveTargetAdapter(target.ConnectorKind)
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
	return transport.OpenLiveConsole(ctx, gateway, LiveRuntime(gateway.runtime, target.ConnectorKind), console.RuntimeOpenRequest{RuntimeID: surface.ID, Rows: rows, Cols: cols, Params: params})
}

type RuntimeActionGateway struct {
	PeerGateway
	runtime *workspaceruntime.Runtime
	kind    string
}

func (component *Component) RuntimeActionPorts(runtime *workspaceruntime.Runtime, kind string) (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
	return RuntimeActionGateway{PeerGateway: component.PeerGateway(), runtime: runtime, kind: kind}, ActionRuntime(runtime, kind)
}

func (gateway RuntimeActionGateway) ConnectorRestartConsoleSession(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
	if err := connectortransport.RequireRuntimeID(ctx, gateway.runtime, gateway.kind, runtimeID); err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	return gateway.component.dependencies.RestartConsole(ctx, gateway.runtime, principal, runtimeID, runningError)
}

func (gateway RuntimeActionGateway) ConnectorCreateAndRunDownloadBatch(ctx context.Context, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
	if err := connectortransport.RequireRuntimeID(ctx, gateway.runtime, gateway.kind, runtimeID); err != nil {
		return connectorapi.TransferBatch{}, err
	}
	return gateway.component.dependencies.RunDownloadBatch(ctx, gateway.runtime, authorization, runtimeID, paths, archiveName, source)
}

type ActionFinishGateway struct {
	component *Component
	runtime   *workspaceruntime.Runtime
	kind      string
}

func (component *Component) ActionFinishPorts(runtime *workspaceruntime.Runtime, kind string) (connectorapi.ActionFinishGateway, connectorapi.ActionRuntime) {
	return ActionFinishGateway{component: component, runtime: runtime, kind: kind}, ActionRuntime(runtime, kind)
}

func (gateway ActionFinishGateway) ConnectorFinishActionRequest(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	if gateway.runtime == nil || gateway.runtime.Storage.Database == nil {
		return connectortargets.ActionRequest{}, ErrRuntimeUnavailable
	}
	request, err := connectortargets.NewStore(gateway.runtime.Storage.Database).GetActionRequest(ctx, requestID)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if request.ConnectorKind != gateway.kind {
		return connectortargets.ActionRequest{}, connectortargets.ErrActionRequestNotFound
	}
	return gateway.component.dependencies.FinishAction(ctx, gateway.runtime, requestID, status, output, displayText, errorText, hints...)
}

type FileTransferGateway struct {
	PeerGateway
	runtime *workspaceruntime.Runtime
	kind    string
}

func (component *Component) FileTransferGateway(runtime *workspaceruntime.Runtime, kind string) connectorapi.FileTransferGateway {
	return FileTransferGateway{PeerGateway: component.PeerGateway(), runtime: runtime, kind: kind}
}

func (gateway FileTransferGateway) ConnectorRuntimeCapabilities() connectors.RuntimeCapabilityResolver {
	return gateway.component.dependencies.RuntimeCapabilities(gateway.kind, gateway.runtime)
}

type TargetDeletionGateway struct {
	PeerGateway
	runtime  *workspaceruntime.Runtime
	kind     string
	targetID int64
}

func (component *Component) TargetDeletionGateway(runtime *workspaceruntime.Runtime, kind string, targetID int64) connectorapi.TargetDeletionGateway {
	return TargetDeletionGateway{PeerGateway: component.PeerGateway(), runtime: runtime, kind: kind, targetID: targetID}
}

func (gateway TargetDeletionGateway) ConnectorRestartConsoleSession(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
	if err := connectortransport.RequireRuntimeID(ctx, gateway.runtime, gateway.kind, runtimeID); err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	return gateway.component.dependencies.RestartConsole(ctx, gateway.runtime, principal, runtimeID, runningError)
}

func (gateway TargetDeletionGateway) ConnectorDeleteTargetRecord(ctx context.Context, target connectortargets.Target, payload map[string]any) error {
	if target.ID != gateway.targetID || target.ConnectorKind != gateway.kind {
		return connectortargets.ErrTargetNotFound
	}
	return gateway.component.dependencies.DeleteTarget(ctx, gateway.runtime, target, payload)
}

func (gateway TargetDeletionGateway) ConnectorFinalizeDeletedTarget(ctx context.Context, target connectortargets.Target, reason string, payload map[string]any) (int64, error) {
	if target.ID != gateway.targetID || target.ConnectorKind != gateway.kind {
		return 0, connectortargets.ErrTargetNotFound
	}
	return gateway.component.dependencies.FinalizeDeletedTarget(ctx, gateway.runtime, target, reason, payload)
}

type TargetOperationGateway struct {
	PeerGateway
	runtime  *workspaceruntime.Runtime
	kind     string
	targetID int64
}

func (component *Component) TargetOperationGateway(runtime *workspaceruntime.Runtime, kind string, targetID int64) connectorapi.TargetOperationGateway {
	return TargetOperationGateway{PeerGateway: component.PeerGateway(), runtime: runtime, kind: kind, targetID: targetID}
}

func (gateway TargetOperationGateway) ConnectorWriteAudit(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	if connectortransport.RequireTargetRuntimeID(ctx, gateway.runtime, gateway.kind, gateway.targetID, runtimeID) == nil {
		gateway.component.dependencies.WriteAudit(ctx, gateway.runtime, actor, tokenID, runtimeID, action, payload)
	}
}

func RequireRuntimeID(ctx context.Context, runtime *workspaceruntime.Runtime, kind string, runtimeID int64) error {
	return connectortransport.RequireRuntimeID(ctx, runtime, kind, runtimeID)
}

func RequireTargetRuntimeID(ctx context.Context, runtime *workspaceruntime.Runtime, kind string, targetID, runtimeID int64) error {
	return connectortransport.RequireTargetRuntimeID(ctx, runtime, kind, targetID, runtimeID)
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
