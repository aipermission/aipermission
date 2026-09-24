package gatewayconnectoractions

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func MCPResponseFromResult(request connectortargets.ActionRequest, result connectors.ActionResult, resolveRunningHint func(connectortargets.ActionRequest) string) Response {
	return wrapResponse(actions.FromResult(request, result, actions.ResponseRunningHint(request, resolveRunningHint)))
}
