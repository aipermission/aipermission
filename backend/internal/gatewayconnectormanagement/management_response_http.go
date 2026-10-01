package gatewayconnectormanagement

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func writeManagementResponse(
	w http.ResponseWriter,
	r *http.Request,
	runtime CredentialRuntimePorts,
	response connectors.ManagementResponse,
	err error,
) {
	writeManagementResponseWithBoundary(w, r, runtime, response, connectormanagement.CredentialBoundary{}, err)
}

func writeManagementResponseWithBoundary(
	w http.ResponseWriter,
	r *http.Request,
	runtime CredentialRuntimePorts,
	response connectors.ManagementResponse,
	boundary connectormanagement.CredentialBoundary,
	err error,
) {
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	status, payload, err := connectormanagement.ProjectManagementResponse(
		r.Context(), runtime.domain(), response, boundary,
	)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, status, payload)
}
