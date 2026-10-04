package api

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) localConnectorActionHTTP() gatewayactions.LocalHTTPHandlers {
	return s.connectorActions.LocalHTTP(gatewayinfra.ConnectorLocalHTTPDependencies{
		ActiveRuntime: s.activeRuntimeOrLocked,
		DecodeJSON:    decodeJSON,
		WriteError:    writeError, WriteErrorCode: writeErrorWithCode, WriteJSON: writeJSON,
		Response: func(request connectormgmt.ActionRequest, result connectors.ActionResult, replayed bool) any {
			response := gatewayactions.MCPResponseFromResult(connectormgmt.ReleaseActionRequest(request), result, s.connectorRuntime.RunningHintPort())
			response.Replayed = replayed
			return response
		},
	})
}
