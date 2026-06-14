package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (r *databaseRuntime) prepareLiveConsoleConnectorAction(ctx context.Context, runtimeID int64, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, r, runtimeID)
	if err != nil {
		return actions.PreparedRequest{}, err
	}
	target, profile, err := connectortargets.NewStore(r.database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return actions.PreparedRequest{}, err
	}
	adapter, ok := connectorAPIAdapterFor(target.ConnectorKind).(connectorapi.LiveConsoleAdapter)
	if !ok || adapter.LiveConsoleActionName() == "" {
		return actions.PreparedRequest{}, connectortargets.ErrInvalidTargetRef
	}
	request.TargetRef = connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID)
	request.ActionName = adapter.LiveConsoleActionName()
	return r.prepareConnectorAction(ctx, request)
}

func liveConsoleTargetRefForRuntimeID(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (string, error) {
	for _, info := range runtime.connectorRegistry().List() {
		adapter := connectorLiveConsoleTargetAdapterFor(info.Kind)
		if adapter == nil {
			continue
		}
		ref, err := adapter.LiveConsoleTargetRef(ctx, runtime, runtimeID)
		if err == nil && ref != "" {
			return ref, nil
		}
	}
	return "", connectortargets.ErrInvalidTargetRef
}
