package gatewayconnectormanagement

import (
	"net/http"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const deletedTargetStaleReason = "connector target was deleted; ask the AI to send a fresh request"

type TargetDeleteHTTPHandler struct{ component *Component }

func (handler *TargetDeleteHTTPHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if handler == nil || handler.component == nil {
		httptransport.WriteInternalError(w)
		return
	}
	workspace, ok := handler.component.active(w)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if workspace.Storage.Database == nil || workspace.Storage.AcquireExclusive == nil || handler.component.dependencies.Adapters == nil {
		httptransport.WriteInternalError(w)
		return
	}
	target, err := handler.component.Catalog(workspace.Storage.Database, workspace.Storage.Registry).Target(r.Context(), id)
	if err != nil {
		WriteTargetError(w, err)
		return
	}
	release, err := workspace.Storage.AcquireExclusive(r.Context())
	if err != nil {
		if release != nil {
			release()
		}
		httptransport.WriteError(w, http.StatusRequestTimeout, "connector target deletion was canceled")
		return
	}
	if release == nil {
		httptransport.WriteInternalError(w)
		return
	}
	defer release()

	adapter, _ := handler.component.dependencies.Adapters.For(target.ConnectorKind).(connectorapi.TargetDeleter)
	if adapter != nil {
		if workspace.Adapters.DeletionGateway == nil || workspace.Adapters.LifecycleRuntime == nil {
			httptransport.WriteInternalError(w)
			return
		}
		gateway := workspace.Adapters.DeletionGateway(target.ConnectorKind, target.ID)
		runtime := workspace.Adapters.LifecycleRuntime(target.ConnectorKind)
		if gateway == nil || runtime == nil {
			httptransport.WriteInternalError(w)
			return
		}
		adapter.DeleteTarget(gateway, w, r, runtime, target)
		return
	}
	if workspace.Lifecycle.DeleteTarget == nil || workspace.Lifecycle.FinalizeTarget == nil {
		httptransport.WriteInternalError(w)
		return
	}
	if err := workspace.Lifecycle.DeleteTarget(r.Context(), target, nil); err != nil {
		WriteTargetError(w, err)
		return
	}
	if _, err := workspace.Lifecycle.FinalizeTarget(r.Context(), target, deletedTargetStaleReason); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
