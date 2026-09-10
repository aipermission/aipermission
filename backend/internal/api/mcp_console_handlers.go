package api

import (
	"context"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

type consoleRestartResult struct {
	ClosedSessionIDs        []int64
	CanceledRunningRequests int64
}

func (s *Server) restartServerConsoleSession(ctx context.Context, runtime *databaseRuntime, principal gatewayaccess.Principal, runtimeID int64, runningRequestError string) (consoleRestartResult, error) {
	if runtime == nil || runtime.Operations.CommandRequests == nil {
		return consoleRestartResult{}, gatewayaccess.ErrCommandRuntimeUnavailable
	}
	var canceledRequests int64
	closedSessionIDs, err := runtime.Connectors.ConsoleSessions.RecoverRuntime(ctx, principal, runtimeID, func() error {
		var err error
		canceledRequests, err = runtime.Operations.CommandRequests.CancelRunningForRuntime(ctx, runtimeID, runningRequestError)
		return err
	})
	if err != nil {
		return consoleRestartResult{}, err
	}
	return consoleRestartResult{
		ClosedSessionIDs:        closedSessionIDs,
		CanceledRunningRequests: canceledRequests,
	}, nil
}
