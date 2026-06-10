package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (r *databaseRuntime) prepareSSHConnectorAction(ctx context.Context, serverID int64, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	targetRef, err := connectortargets.NewStore(r.database).SSHTargetRefForServer(ctx, serverID)
	if err != nil {
		return actions.PreparedRequest{}, err
	}
	request.TargetRef = targetRef
	return r.prepareConnectorAction(ctx, request)
}
