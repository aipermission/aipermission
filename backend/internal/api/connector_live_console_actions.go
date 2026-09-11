package api

import (
	"context"
	"errors"
	"fmt"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func prepareLiveConsoleConnectorAction(runtime databaseRuntime, ctx context.Context, runtimeID int64, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return actions.PreparedRequest{}, err
	}
	target, profile, err := connectormgmt.NewStore(runtime.StoragePort().DatabaseHandle()).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return actions.PreparedRequest{}, err
	}
	adapter, ok := runtimeConnectorAPIAdapterFor(runtime, target.ConnectorKind).(connectorapi.LiveConsoleAdapter)
	if !ok || adapter.LiveConsoleActionName() == "" {
		return actions.PreparedRequest{}, connectormgmt.ErrInvalidTargetRef
	}
	request.TargetRef = connectorapi.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID)
	request.ActionName = adapter.LiveConsoleActionName()
	return prepareConnectorAction(runtime, ctx, request)
}

func liveConsoleTargetRefForRuntimeID(ctx context.Context, runtime databaseRuntime, runtimeID int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, info := range runtimeConnectorRegistry(runtime).List() {
		adapter, _ := runtimeConnectorAPIAdapterFor(runtime, info.Kind).(connectorapi.LiveConsoleTargetAdapter)
		if adapter == nil {
			continue
		}
		ref, err := adapter.LiveConsoleTargetRef(ctx, connectorLiveRuntime(runtime, info.Kind), runtimeID)
		if errors.Is(err, connectormgmt.ErrRuntimeSurfaceNotFound) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("resolve %s live console runtime: %w", info.Kind, err)
		}
		if ref == "" {
			return "", fmt.Errorf("resolve %s live console runtime: empty target reference", info.Kind)
		}
		return ref, nil
	}
	return "", connectormgmt.ErrInvalidTargetRef
}
