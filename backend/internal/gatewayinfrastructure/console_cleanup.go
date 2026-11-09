package gatewayinfrastructure

import (
	"context"
)

func (component *OperationsOwner) InitializeConsoleCleanup(handle *WorkspaceHandle) {
	capabilities, available := component.projection(handle)
	if !available {
		return
	}
	capability, ok := capabilities.LiveConsole.Current()
	if !ok || capability.Sessions == nil {
		return
	}
	identity := handle.Identity()
	capability.Sessions.ConfigureIdleCleanup(identity.WorkspaceID, identity.RuntimeID, func(ctx context.Context, action string, payload any) error {
		return component.observation.writeObservationRequired(ctx, handle, "system", nil, 0, action, payload)
	})
}
