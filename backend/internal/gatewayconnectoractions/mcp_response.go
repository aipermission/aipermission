package gatewayconnectoractions

import (
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func MCPResponseFromResult(request connectortargets.ActionRequest, result connectors.ActionResult, resolveRunningHint func(connectortargets.ActionRequest) string) actions.Response {
	runningHint := ""
	if request.Status == connectors.ResultRunning {
		if resolveRunningHint != nil {
			runningHint = strings.TrimSpace(resolveRunningHint(request))
		}
		if runningHint == "" {
			runningHint = "Wait 3 seconds, then call get_connector_action_request again until this request is completed, failed, canceled, stale, or error."
		}
	}
	return actions.FromResult(request, result, runningHint)
}
