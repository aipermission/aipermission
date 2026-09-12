package api

import (
	"net/http"

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
		HandleTargetError: connectormgmt.WriteTargetError,
		Response: func(request connectormgmt.ActionRequest, result connectors.ActionResult, replayed bool) any {
			response := gatewayactions.MCPResponseFromResult(connectormgmt.ReleaseActionRequest(request), result, s.connectorRuntime.RunningHintPort())
			response.Replayed = replayed
			return response
		},
	})
}

func writeConnectorActionTerminalPersistenceError(w http.ResponseWriter, err error) bool {
	requestID, ok := gatewayactions.TerminalPersistenceRequestID(err)
	if !ok {
		return false
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"status": connectors.ResultOutcomeUnknown, "code": "connector_action_persistence_unknown",
		"request_id": requestID, "error": gatewayactions.TerminalPersistenceErrorText(),
		"assistant_hint": "Do not retry automatically. Inspect the recorded request and external target state first.",
	})
	return true
}
