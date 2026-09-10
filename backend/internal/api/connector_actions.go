package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type connectorActionTargetResolver struct {
	store *connectortargets.Store
}

func newConnectorActionTargetResolver(database *sql.DB) connectorActionTargetResolver {
	return connectorActionTargetResolver{store: connectortargets.NewStore(database)}
}

func (r connectorActionTargetResolver) ResolveActionTarget(ctx context.Context, targetRef string) (actions.ResolvedTarget, error) {
	target, profile, err := r.store.ResolveConnectorActionTarget(ctx, targetRef)
	if err == nil {
		return actions.ResolvedTarget{Target: target, Profile: profile}, nil
	}
	if errors.Is(err, connectortargets.ErrInvalidTargetRef) || errors.Is(err, connectortargets.ErrTargetNotFound) || errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
		return actions.ResolvedTarget{}, actions.ErrTargetNotFound
	}
	return actions.ResolvedTarget{}, err
}

func prepareConnectorAction(runtime *databaseRuntime, ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	if runtime == nil || runtime.Storage.Database == nil {
		return actions.PreparedRequest{}, fmt.Errorf("database runtime is not available")
	}
	service := actions.NewService(runtimeConnectorRegistry(runtime), newConnectorActionTargetResolver(runtime.Storage.Database))
	return service.Prepare(ctx, request)
}
