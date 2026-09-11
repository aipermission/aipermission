package api

import (
	"net/http"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s connectorTargetHandlers) deleteConnectorTarget(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	store := connectormgmt.NewStore(runtime.StoragePort().DatabaseHandle())
	target, err := store.GetTarget(r.Context(), id)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	release, err := runtime.SecurityPort().VaultDeliveryCoordinator().AcquireExclusive(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "connector target deletion was canceled")
		return
	}
	defer release()
	if adapter := s.connectorTargetDeleterFor(target.ConnectorKind); adapter != nil {
		adapter.DeleteTarget(
			s.connectorPortsApplication().TargetDeletionGateway(s.connectorPortsWorkspace(runtime), target.ConnectorKind, target.ID),
			w, r, s.connectorTargetLifecycleRuntime(runtime, target.ConnectorKind), target,
		)
		return
	}
	if err := s.connectorDeleteTargetRecord(r.Context(), runtime, target, nil); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if _, err := s.connectorFinalizeDeletedTarget(r.Context(), runtime, target, "connector target was deleted; ask the AI to send a fresh request", nil); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s connectorTargetHandlers) finalizeDeletedConnectorTarget(w http.ResponseWriter, r *http.Request, runtime databaseRuntime, target connectormgmt.Target, staleReason string, payload map[string]any) bool {
	_, err := s.connectorFinalizeDeletedTarget(r.Context(), runtime, target, staleReason, payload)
	if err != nil {
		writeInternalError(w)
		return false
	}
	return true
}
