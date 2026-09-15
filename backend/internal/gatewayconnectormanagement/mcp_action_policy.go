package gatewayconnectormanagement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type MCPActionResourcePolicy struct {
	MaxInputBytes  int
	PersistedRows  int64
	PersistedBytes int64
}

func (catalog Catalog) MCPActionVisible(ctx context.Context, tokenID int64, targetRef, actionName string) (bool, error) {
	permissions, err := catalog.ProjectScopedSupportedConnectorPermissions(ctx, tokenID)
	if err != nil {
		return false, err
	}
	for _, permission := range permissions {
		if permission.ExecutionRule != ActionPermissionBlocked && permission.ActionName == actionName &&
			connectors.FormatTargetRef(permission.ConnectorKind, permission.TargetID, permission.ProfileID) == targetRef {
			return true, nil
		}
	}
	return false, nil
}

func (catalog Catalog) MCPReplayExists(ctx context.Context, tokenID int64, idempotencyKey string) (bool, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return false, nil
	}
	requestTokenID := tokenID
	_, err := catalog.store().GetActionRequestByIdempotency(ctx, &requestTokenID, "mcp", idempotencyKey)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		return false, nil
	}
	return false, err
}

func (catalog Catalog) MCPActionResourcePolicy(ctx context.Context, tokenID int64, targetRef, actionName string) (MCPActionResourcePolicy, error) {
	target, profile, err := catalog.store().ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return MCPActionResourcePolicy{}, err
	}
	connector, ok := catalog.registry.Get(target.ConnectorKind)
	if !ok {
		return MCPActionResourcePolicy{}, fmt.Errorf("connector %q is not registered", target.ConnectorKind)
	}
	definitions, err := connectors.GetActionDefinitions(ctx, connector, target, profile)
	if err != nil {
		return MCPActionResourcePolicy{}, err
	}
	policy := MCPActionResourcePolicy{}
	for _, definition := range definitions {
		if definition.Name == actionName {
			policy.MaxInputBytes = definition.MaxInputBytes
			break
		}
	}
	if policy.MaxInputBytes == 0 {
		return MCPActionResourcePolicy{}, fmt.Errorf("connector action %q is not registered", actionName)
	}
	return policy, nil
}
