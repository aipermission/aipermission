package api

import (
	"context"
)

type consoleRestartResult struct {
	ClosedSessionIDs        []int64
	CanceledRunningRequests int64
}

func (s *Server) restartServerConsoleSession(ctx context.Context, runtime *databaseRuntime, serverID int64, runningRequestError string) (consoleRestartResult, error) {
	sessions, err := runtime.consoleSessions.List(ctx, serverID)
	if err != nil {
		return consoleRestartResult{}, err
	}
	closedSessionIDs := []int64{}
	for _, session := range sessions {
		if session.Status == "connecting" || session.Status == "connected" {
			closedSessionIDs = append(closedSessionIDs, session.ID)
		}
	}

	canceledRequests, err := s.cancelRunningCommandRequestsForServer(ctx, runtime, serverID, runningRequestError)
	if err != nil {
		return consoleRestartResult{}, err
	}
	if err := runtime.consoleSessions.CloseServer(ctx, serverID); err != nil {
		return consoleRestartResult{}, err
	}
	return consoleRestartResult{
		ClosedSessionIDs:        closedSessionIDs,
		CanceledRunningRequests: canceledRequests,
	}, nil
}
