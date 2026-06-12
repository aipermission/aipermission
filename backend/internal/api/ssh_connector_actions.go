package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (r *databaseRuntime) prepareSSHConnectorAction(ctx context.Context, serverID int64, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	target, profile, err := connectortargets.NewStore(r.database).SSHRuntimeForConsoleID(ctx, serverID)
	if err != nil {
		return actions.PreparedRequest{}, err
	}
	request.TargetRef = connectortargets.SSHTargetRef(target.ID, profile.ID)
	return r.prepareConnectorAction(ctx, request)
}
