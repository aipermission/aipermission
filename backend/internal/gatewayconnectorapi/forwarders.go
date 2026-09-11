package gatewayconnectorapi

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
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
