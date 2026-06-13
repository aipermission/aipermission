package api

import (
	"context"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (runtime *databaseRuntime) prepareConnectorAction(ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	if runtime == nil || runtime.database == nil {
		return actions.PreparedRequest{}, fmt.Errorf("database runtime is not available")
	}
	service := actions.NewService(runtime.connectorRegistry(), connectortargets.NewResolver(runtime.database))
	return service.Prepare(ctx, request)
}
