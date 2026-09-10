package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type consoleRestartResult struct {
	ClosedSessionIDs        []int64
	CanceledRunningRequests int64
}

func (s *Server) restartServerConsoleSession(ctx context.Context, runtime *databaseRuntime, principal executionprincipal.Principal, runtimeID int64, runningRequestError string) (consoleRestartResult, error) {
	if runtime == nil || runtime.commandRequests == nil {
		return consoleRestartResult{}, commandrequests.ErrRuntimeUnavailable
	}
	var canceledRequests int64
	closedSessionIDs, err := runtime.consoleSessions.RecoverRuntime(ctx, principal, runtimeID, func() error {
		var err error
		canceledRequests, err = runtime.commandRequests.CancelRunningForRuntime(ctx, runtimeID, runningRequestError)
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
