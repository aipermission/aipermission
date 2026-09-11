package gatewayconnectorapi

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	connectorports "github.com/aipermission/aipermission/backend/internal/applicationconnectorports"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func NewLiveConsoleHTTPHandlers(scope connectorapi.LiveConsoleHTTPScopeProvider) *connectorapi.LiveConsoleHTTPHandlers {
	return connectorapi.NewLiveConsoleHTTPHandlers(scope)
}

func NewRegistry() *connectorapi.Registry {
	return connectorapi.NewRegistry()
}

func PresentedErrorMessage(adapter any, prefix string, err error) string {
	return connectorapi.PresentedErrorMessage(adapter, prefix, err)
}

func MCPResponseFromResult(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest, result connectors.ActionResult) actions.Response {
	return mcpconnector.ResponseFromResult(adapterRegistry, request, result)
}

func NewPorts(dependencies connectorports.Dependencies) *connectorports.Component {
	return connectorports.New(dependencies)
}

func DataRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.ConnectorDataRuntime {
	return connectorports.DataRuntime(runtime, kind)
}

func LiveRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.LiveConsoleRuntime {
	return connectorports.LiveRuntime(runtime, kind)
}

func PortActionRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.ActionRuntime {
	return connectorports.ActionRuntime(runtime, kind)
}

func PortCredentialResourceRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.CredentialResourceRuntime {
	return connectorports.CredentialResourceRuntime(runtime, kind)
}

func RequireRuntimeID(ctx context.Context, runtime *workspaceruntime.Runtime, kind string, runtimeID int64) error {
	return connectorports.RequireRuntimeID(ctx, runtime, kind, runtimeID)
}

func RequireTargetRuntimeID(ctx context.Context, runtime *workspaceruntime.Runtime, kind string, targetID int64, runtimeID int64) error {
	return connectorports.RequireTargetRuntimeID(ctx, runtime, kind, targetID, runtimeID)
}
