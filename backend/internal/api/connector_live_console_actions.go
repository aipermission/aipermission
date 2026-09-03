package api

import (
	"context"
	"errors"
	"fmt"

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
	adapter, ok := r.connectorAPIAdapterFor(target.ConnectorKind).(connectorapi.LiveConsoleAdapter)
	if !ok || adapter.LiveConsoleActionName() == "" {
		return actions.PreparedRequest{}, connectortargets.ErrInvalidTargetRef
	}
	request.TargetRef = connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID)
	request.ActionName = adapter.LiveConsoleActionName()
	return r.prepareConnectorAction(ctx, request)
}

func liveConsoleTargetRefForRuntimeID(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, info := range runtime.connectorRegistry().List() {
		adapter, _ := runtime.connectorAPIAdapterFor(info.Kind).(connectorapi.LiveConsoleTargetAdapter)
		if adapter == nil {
			continue
		}
		ref, err := adapter.LiveConsoleTargetRef(ctx, runtime, runtimeID)
		if errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
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
	return "", connectortargets.ErrInvalidTargetRef
}
