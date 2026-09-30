package connectorports

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

// Target management does not require an execution surface. Bind audit identity
// to the scoped gateway, never to a caller-supplied payload or runtime ID.
func (gateway TargetOperationGateway) ConnectorWriteTargetAudit(ctx context.Context, action string, payload any) error {
	if gateway.workspace.runtime.Database == nil || gateway.workspace.Targets.TargetAudit == nil {
		return ErrRuntimeUnavailable
	}
	target, err := connectortargets.NewStore(gateway.workspace.runtime.Database).GetTarget(ctx, gateway.targetID)
	if err != nil {
		return err
	}
	if target.ConnectorKind != gateway.kind {
		return connectortargets.ErrTargetNotFound
	}
	return gateway.workspace.Targets.TargetAudit(ctx, action, map[string]any{
		"target_id": target.ID, "connector_kind": target.ConnectorKind, "details": payload,
	})
}
