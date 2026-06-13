package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (r *databaseRuntime) prepareLiveConsoleConnectorAction(ctx context.Context, profileID int64, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	targetRef, err := liveConsoleTargetRef(ctx, r, profileID)
	if err != nil {
		return actions.PreparedRequest{}, err
	}
	target, profile, err := connectortargets.NewStore(r.database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return actions.PreparedRequest{}, err
	}
	adapter, ok := connectorAPIAdapterFor(target.ConnectorKind).(connectorLiveConsoleAdapter)
	if !ok || adapter.LiveConsoleActionName() == "" {
		return actions.PreparedRequest{}, connectortargets.ErrInvalidTargetRef
	}
	request.TargetRef = connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID)
	request.ActionName = adapter.LiveConsoleActionName()
	return r.prepareConnectorAction(ctx, request)
}

func liveConsoleTargetRef(ctx context.Context, runtime *databaseRuntime, profileID int64) (string, error) {
	var connectorKind string
	var targetID int64
	err := runtime.database.QueryRowContext(ctx, `
		SELECT connector_kind, target_id
		FROM connector_credential_profiles
		WHERE id = ? AND status = 'active'`,
		profileID,
	).Scan(&connectorKind, &targetID)
	if err != nil {
		return "", err
	}
	return connectortargets.ConnectorTargetRef(connectorKind, targetID, profileID), nil
}
