package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

var ErrWorkspaceHandleUnavailable = errors.New("workspace handle is unavailable")

type ConnectorActionPorts struct {
	Capabilities    func(*WorkspaceHandle, string, []connectors.ResolvedDependency, ConnectorActionFinishPort) connectors.RuntimeCapabilityResolver
	SupportsRunning func(gatewayactions.PreparedRequest) bool
	FinishRunning   func(context.Context, *WorkspaceHandle, int64, gatewayactions.PreparedRequest, gatewayaccess.Principal, connectors.ActionHandles, ConnectorActionFinishPort)
}

type ConnectorActionApplication struct {
	owner       *ConnectorActionOwner
	application *gatewayactions.Component
	ports       ConnectorActionPorts
}

func (component *ConnectorActionOwner) NewConnectorActionApplication(maxJSONBytes int, ports ConnectorActionPorts) (*ConnectorActionApplication, error) {
	if component == nil || component.owner == nil || component.observation == nil || ports.Capabilities == nil || ports.SupportsRunning == nil || ports.FinishRunning == nil {
		return nil, InitializationError()
	}
	application := &ConnectorActionApplication{owner: component, ports: ports}
	application.application = gatewayactions.New(gatewayactions.Dependencies{
		MaxJSONBytes: maxJSONBytes,
		SupportsRunning: func(prepared gatewayactions.PreparedRequest) bool {
			return application.ports.SupportsRunning(prepared)
		},
	})
	return application, nil
}

func (component *ConnectorActionApplication) workspace(handle *WorkspaceHandle) (gatewayactions.Workspace, bool) {
	if component == nil || component.owner == nil {
		return gatewayactions.Workspace{}, false
	}
	capabilities, available := component.owner.projection(handle)
	if !available || component.application == nil {
		return gatewayactions.Workspace{}, false
	}
	projected := capabilities.Action
	capability, ok := projected.Current()
	if !ok || capability.Policy == nil || capability.Delivery == nil || capability.Control == nil || capabilities.Tag == nil {
		return gatewayactions.Workspace{}, false
	}
	identity := handle.Identity()
	workflow := gatewayactions.WorkflowPorts{
		AcquireSecret: capability.Delivery.AcquireDelivery,
		RedactBasic: func(ctx context.Context, value string) string {
			return capability.Policy.Redact(ctx, value)
		},
		RedactCustom: func(ctx context.Context, value string) string {
			return capability.Policy.RedactCustom(ctx, value)
		},
		Mutate: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return component.owner.observation.withObservationMutation(ctx, handle, actor, tokenID, runtimeID, action, payload, mutate)
		},
		Transaction: func(ctx context.Context, mutate func(*sql.Tx, gatewayactions.AuditAppender) error) error {
			return component.owner.observation.withObservationTransaction(ctx, handle, func(tx *sql.Tx, appendAudit observationAppender) error {
				return mutate(tx, gatewayactions.AuditAppender(appendAudit))
			})
		},
		Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			component.owner.observation.writeObservation(ctx, handle, actor, tokenID, runtimeID, action, payload)
		},
		Capabilities: func(kind string, dependencies []connectors.ResolvedDependency) connectors.RuntimeCapabilityResolver {
			return component.ports.Capabilities(handle, kind, dependencies, component.finishPort())
		},
		FinishRunning: func(ctx context.Context, id int64, prepared gatewayactions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles) {
			component.ports.FinishRunning(ctx, handle, id, prepared, principal, handles, component.finishPort())
		},
	}
	return gatewayactions.Workspace{
		Storage: gatewayactions.ActionStorage{
			Database: capability.Database, Tokens: capability.Tokens,
			Registry: capability.Registry, SecretVault: capability.Vault,
			WorkspaceID: identity.WorkspaceID,
		},
		Identity: gatewayactions.ActionIdentity{
			Tag: capabilities.Tag, RuntimeInstanceID: identity.RuntimeID,
			MCPStarted: capability.Control.MCPStarted,
			Ensure: func() error {
				if !component.owner.valid(handle) || !handle.Identity().Ready() {
					return ErrWorkspaceHandleUnavailable
				}
				return nil
			},
		},
		Workflow: workflow,
	}, true
}

func (component *ConnectorActionApplication) finishPort() ConnectorActionFinishPort {
	return func(ctx context.Context, handle *WorkspaceHandle, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
		return component.Finish(ctx, handle, requestID, status, output, displayText, errorText, hints...)
	}
}

func (component *ConnectorActionApplication) Call(ctx context.Context, handle *WorkspaceHandle, call gatewayactions.Call) (gatewayactions.CallResult, error) {
	workspace, _ := component.workspace(handle)
	return component.application.Call(ctx, workspace, call)
}

func (component *ConnectorActionApplication) RunLocal(ctx context.Context, handle *WorkspaceHandle, call gatewayactions.Call) (gatewayactions.CallResult, error) {
	workspace, _ := component.workspace(handle)
	return component.application.RunLocal(ctx, workspace, call)
}

func (component *ConnectorActionApplication) MCPCall(handle *WorkspaceHandle) func(context.Context, gatewayactions.Call) (gatewayactions.CallResult, error) {
	workspace, _ := component.workspace(handle)
	return component.application.MCPCall(workspace)
}

func (component *ConnectorActionApplication) Approval(handle *WorkspaceHandle) (gatewayactions.ApprovalWorkflow, error) {
	workspace, _ := component.workspace(handle)
	return component.application.Approval(workspace)
}

func (component *ConnectorActionApplication) Shutdown(handle *WorkspaceHandle) (gatewayactions.ShutdownWorkflow, error) {
	workspace, _ := component.workspace(handle)
	return component.application.Shutdown(workspace)
}

func (component *ConnectorActionApplication) Finish(ctx context.Context, handle *WorkspaceHandle, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
	workspace, _ := component.workspace(handle)
	request, err := component.application.Finish(ctx, workspace, requestID, status, output, displayText, errorText, hints...)
	return connectormgmt.AdoptActionRequest(request), err
}

func (component *ConnectorActionApplication) StartRecovery(handle *WorkspaceHandle) {
	workspace, _ := component.workspace(handle)
	component.application.StartRecovery(workspace)
}

func (component *ConnectorActionApplication) StopRecovery(handle *WorkspaceHandle) {
	workspace, _ := component.workspace(handle)
	component.application.StopRecovery(workspace)
}

func (component *ConnectorActionApplication) Release(handle *WorkspaceHandle) {
	workspace, _ := component.workspace(handle)
	component.application.ReleaseWorkspace(workspace)
}

func (component *ConnectorActionApplication) Tag(handle *WorkspaceHandle, value []byte) (string, error) {
	workspace, ok := component.workspace(handle)
	if !ok || workspace.Identity.Tag == nil {
		return "", ErrWorkspaceHandleUnavailable
	}
	return workspace.Identity.Tag(value)
}

func (component *ConnectorActionApplication) Delivery(acquire func(context.Context) (func(), error)) gatewayactions.DeliveryGate {
	return component.application.Delivery(acquire)
}

func (component *ConnectorActionApplication) Persistence(handle *WorkspaceHandle) (gatewayactions.RequestPersistence, error) {
	workspace, _ := component.workspace(handle)
	return component.application.Persistence(workspace)
}

func (component *ConnectorActionApplication) Dispatch(handle *WorkspaceHandle) (gatewayactions.DispatchWorkflow, error) {
	workspace, _ := component.workspace(handle)
	return component.application.Dispatch(workspace)
}

func (component *ConnectorActionApplication) Recovery(handle *WorkspaceHandle) (gatewayactions.RecoveryWorkflow, error) {
	workspace, _ := component.workspace(handle)
	return component.application.Recovery(workspace)
}

func (component *ConnectorActionApplication) RedactValue(ctx context.Context, handle *WorkspaceHandle, value any, sensitiveFields, capabilityFields map[string]bool, boundary gatewayactions.CredentialBoundary) (any, error) {
	workspace, _ := component.workspace(handle)
	return component.application.RedactValue(ctx, workspace, value, sensitiveFields, capabilityFields, boundary)
}

func (component *ConnectorActionApplication) RedactResult(ctx context.Context, handle *WorkspaceHandle, result connectors.ActionResult, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	workspace, _ := component.workspace(handle)
	return component.application.RedactResult(ctx, workspace, result, hints...)
}

func (component *ConnectorActionApplication) RedactResultWithCredentialBoundary(ctx context.Context, handle *WorkspaceHandle, result connectors.ActionResult, boundary gatewayactions.CredentialBoundary, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	workspace, _ := component.workspace(handle)
	return component.application.RedactResultWithCredentialBoundary(ctx, workspace, result, boundary, hints...)
}

func (component *ConnectorActionApplication) RedactInput(ctx context.Context, handle *WorkspaceHandle, input map[string]any, sensitiveFields []string) (map[string]any, error) {
	workspace, _ := component.workspace(handle)
	return component.application.RedactInput(ctx, workspace, input, sensitiveFields)
}

func (component *ConnectorActionApplication) RedactPreview(ctx context.Context, handle *WorkspaceHandle, preview map[string]any, sensitiveFields []string, hints ...connectors.OutputHint) (map[string]any, error) {
	workspace, _ := component.workspace(handle)
	return component.application.RedactPreview(ctx, workspace, preview, sensitiveFields, hints...)
}

func (component *ConnectorActionApplication) SensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	return component.application.SensitiveOutputFields(hints...)
}

type ConnectorLocalHTTPDependencies struct {
	ActiveRuntime     func(http.ResponseWriter) (*WorkspaceHandle, bool)
	DecodeJSON        func(http.ResponseWriter, *http.Request, any) bool
	WriteError        func(http.ResponseWriter, int, string)
	WriteErrorCode    func(http.ResponseWriter, int, string, string)
	WriteJSON         func(http.ResponseWriter, int, any)
	HandleTargetError func(http.ResponseWriter, error)
	Response          func(connectormgmt.ActionRequest, connectors.ActionResult, bool) any
}

func (component *ConnectorActionApplication) LocalHTTP(dependencies ConnectorLocalHTTPDependencies) gatewayactions.LocalHTTPHandlers {
	return component.application.LocalHTTP(gatewayactions.LocalHTTPDependencies{
		ActiveRuntime: func(w http.ResponseWriter) (gatewayactions.Workspace, bool) {
			handle, ok := dependencies.ActiveRuntime(w)
			if !ok {
				return gatewayactions.Workspace{}, false
			}
			return component.workspace(handle)
		},
		DecodeJSON: dependencies.DecodeJSON, WriteError: dependencies.WriteError,
		WriteErrorCode: dependencies.WriteErrorCode, WriteJSON: dependencies.WriteJSON,
		HandleTargetError: dependencies.HandleTargetError,
		Response:          connectormgmt.DomainActionResponse(dependencies.Response),
	})
}
