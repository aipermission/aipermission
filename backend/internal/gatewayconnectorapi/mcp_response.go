package gatewayconnectorapi

import (
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

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
