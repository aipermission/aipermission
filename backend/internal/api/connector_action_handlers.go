package api

import (
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actions"
	applicationactions "github.com/aipermission/aipermission/backend/internal/applicationconnectoractions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
)

type localConnectorActionRequest = applicationactions.LocalRequest

func (s *Server) localConnectorActionHTTP() applicationactions.LocalHTTPHandlers {
	return s.connectorActionApplication().LocalHTTP(applicationactions.LocalHTTPDependencies{
		ActiveRuntime: s.activeRuntimeOrLocked, DecodeJSON: decodeJSON,
		WriteError: writeError, WriteErrorCode: writeErrorWithCode, WriteJSON: writeJSON,
		HandleTargetError: handleConnectorTargetError,
		Response: func(request connectortargets.ActionRequest, result connectors.ActionResult, replayed bool) any {
			response := mcpconnector.ResponseFromResult(s.connectorAdapterRegistry(), request, result)
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
