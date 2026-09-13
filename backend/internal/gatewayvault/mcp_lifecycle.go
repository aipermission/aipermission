package gatewayvault

import (
	"context"
	"errors"
)

const (
	mcpStoppedRequestReason = "MCP execution stopped; send a fresh Vault request after it starts"
	mcpStoppedRunningReason = "MCP execution stopped while the Vault action was running"
)

type MCPStopLifecycle interface {
	InvalidateAll(context.Context, string) error
}

type MCPStopRequests interface {
	StalePendingForAction(context.Context, string, string) error
	FailRunning(context.Context, string) error
}

func (component *Component) StopMCP(ctx context.Context, lifecycle MCPStopLifecycle, requests MCPStopRequests) error {
	if component == nil || lifecycle == nil || requests == nil {
		return InvalidatorUnavailableError()
	}
	invalidateErr := lifecycle.InvalidateAll(ctx, mcpStoppedRequestReason)
	staleErr := requests.StalePendingForAction(ctx, ActionGenerateItem, mcpStoppedRequestReason)
	failErr := requests.FailRunning(ctx, mcpStoppedRunningReason)
	return errors.Join(invalidateErr, staleErr, failErr)
}
