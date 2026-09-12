package gatewayvault

import "context"

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
	if err := lifecycle.InvalidateAll(ctx, mcpStoppedRequestReason); err != nil {
		return err
	}
	if err := requests.StalePendingForAction(ctx, ActionGenerateItem, mcpStoppedRequestReason); err != nil {
		return err
	}
	return requests.FailRunning(ctx, mcpStoppedRunningReason)
}
