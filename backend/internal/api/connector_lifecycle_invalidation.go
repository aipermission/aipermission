package api

import "context"

func (s connectorTargetHandlers) afterConnectorCredentialLifecycleChange(
	ctx context.Context,
	runtime *databaseRuntime,
	targetID int64,
	profileID int64,
	vaultReason string,
	requestReason string,
	includeRunning bool,
) error {
	if err := s.invalidateVaultSessionsForTargetProfile(ctx, runtime, targetID, profileID, vaultReason); err != nil {
		return err
	}
	_, err := s.staleConnectorActionRequestsForTarget(ctx, runtime, targetID, profileID, requestReason, includeRunning)
	return err
}
