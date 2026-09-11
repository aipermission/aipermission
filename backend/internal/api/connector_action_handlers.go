package api

import (
	"errors"
	"net/http"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
)

type localConnectorActionRequest = actions.LocalRequest

func (s *Server) localConnectorActionHTTP() actions.LocalHTTPHandlers {
	return s.connectorActionApplication().LocalHTTP(actions.LocalHTTPDependencies{
		ActiveRuntime: func(w http.ResponseWriter) (actions.Workspace, bool) {
			runtime, ok := s.activeRuntimeOrLocked(w)
			return s.connectorActionWorkspace(runtime), ok
		},
		DecodeJSON: decodeJSON,
		WriteError: writeError, WriteErrorCode: writeErrorWithCode, WriteJSON: writeJSON,
		HandleTargetError: handleConnectorTargetError,
		Response: func(request connectormgmt.ActionRequest, result connectors.ActionResult, replayed bool) any {
			response := connectorapi.MCPResponseFromResult(s.connectorAdapterRegistry(), request, result)
			response.Replayed = replayed
			return response
		},
	})
}

func writeConnectorActionTerminalPersistenceError(w http.ResponseWriter, err error) bool {
	var persistence *actions.TerminalPersistenceError
	if !errors.As(err, &persistence) {
		return false
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"status": connectors.ResultOutcomeUnknown, "code": "connector_action_persistence_unknown",
		"request_id": persistence.RequestID, "error": actions.TerminalPersistenceErrorText,
		"assistant_hint": "Do not retry automatically. Inspect the recorded request and external target state first.",
	})
	return true
}
