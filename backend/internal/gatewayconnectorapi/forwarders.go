package gatewayconnectorapi

import (
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
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
	runningHint := ""
	if request.Status == connectors.ResultRunning {
		adapter, _ := adapterRegistry.For(request.ConnectorKind).(connectorapi.RuntimeAdapter)
		if adapter != nil {
			runningHint = strings.TrimSpace(adapter.RunningHint(request))
		}
		if runningHint == "" {
			runningHint = "Wait 3 seconds, then call get_connector_action_request again until this request is completed, failed, canceled, stale, or error."
		}
	}
	return actions.FromResult(request, result, runningHint)
}

func ErrorCode(err error) string { return connectors.ErrorCode(err) }

func ErrorStatus(err error) connectors.ResultStatus { return connectors.ErrorStatus(err) }

func FormatTargetRef(connectorKind string, targetID int64, profileID int64) string {
	return connectors.FormatTargetRef(connectorKind, targetID, profileID)
}

func NewConnectorRegistry() *connectors.Registry { return connectors.NewRegistry() }
