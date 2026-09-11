package api

import (
	"net/http"
	"strings"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s connectorTargetHandlers) runConnectorTargetOperation(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	operation := strings.TrimSpace(r.PathValue("operation"))
	target, err := s.connectorCatalog(runtime).Target(r.Context(), targetID)
	if err != nil {
		connectormgmt.WriteTargetError(w, err)
		return
	}
	adapter := s.connectorTargetOperationRunnerFor(target.ConnectorKind)
	if adapter == nil {
		writeError(w, http.StatusBadRequest, "operation is not supported for this connector")
		return
	}
	adapter.RunTargetOperation(
		s.connectorPortsApplication().TargetOperationGateway(s.connectorPortsWorkspace(runtime), target.ConnectorKind, target.ID),
		w, r, s.connectorDataRuntimePort(runtime, target.ConnectorKind), target, operation,
	)
}
