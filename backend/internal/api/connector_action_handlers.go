package api

import (
	"errors"
	"net/http"

	domainactions "github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

type localConnectorActionRequest = gatewayactions.LocalRequest

func (s *Server) localConnectorActionHTTP() gatewayactions.LocalHTTPHandlers {
	return s.connectorActionApplication().LocalHTTP(gatewayactions.LocalHTTPDependencies{
		ActiveRuntime: func(w http.ResponseWriter) (gatewayactions.Workspace, bool) {
			runtime, ok := s.activeRuntimeOrLocked(w)
			return s.connectorActionWorkspace(runtime), ok
		},
		DecodeJSON: decodeJSON,
		WriteError: writeError, WriteErrorCode: writeErrorWithCode, WriteJSON: writeJSON,
		HandleTargetError: connectormgmt.WriteTargetError,
		Response: func(request connectormgmt.ActionRequest, result connectors.ActionResult, replayed bool) any {
			response := gatewayactions.MCPResponseFromResult(request, result, s.connectorRunningHint)
			response.Replayed = replayed
			return response
		},
	})
}

func writeConnectorActionTerminalPersistenceError(w http.ResponseWriter, err error) bool {
	var persistence *domainactions.TerminalPersistenceError
	if !errors.As(err, &persistence) {
		return false
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"status": connectors.ResultOutcomeUnknown, "code": "connector_action_persistence_unknown",
		"request_id": persistence.RequestID, "error": domainactions.TerminalPersistenceErrorText,
		"assistant_hint": "Do not retry automatically. Inspect the recorded request and external target state first.",
	})
	return true
}
