package gatewayinfrastructure

import (
	"context"
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

type AccessControlPorts struct {
	FinishTokenInvalidation func(context.Context, int64, []int64)
}

func (component *AccessOwner) CanReadVaultMetadata(
	ctx context.Context,
	handle *WorkspaceHandle,
	factory gatewayaccess.VaultMetadataReaderFactory,
	tokenID int64,
	projectID int64,
	now time.Time,
) (bool, error) {
	capabilities, available := component.projection(handle)
	if !available || factory == nil {
		return false, gatewayaccess.ErrVaultMetadataAccessUnavailable
	}
	projected := capabilities.VaultMetadata
	capability, ok := projected.Current()
	if !ok {
		return false, gatewayaccess.ErrVaultMetadataAccessUnavailable
	}
	reader := factory.ForDatabase(capability.Database)
	if reader == nil {
		return false, gatewayaccess.ErrVaultMetadataAccessUnavailable
	}
	return reader.CanRead(ctx, tokenID, projectID, now)
}

func (component *AccessOwner) accessControlWorkspace(handle *WorkspaceHandle, ports AccessControlPorts) (gatewayaccess.AccessScope, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayaccess.AccessScope{}, false
	}
	projected := capabilities.AccessControl
	capability, ok := projected.Current()
	if !ok || capability.Policy == nil || capability.Delivery == nil {
		return gatewayaccess.AccessScope{}, false
	}
	return gatewayaccess.AccessScope{
		Database: capability.Database, Tokens: capability.Tokens,
		Registry: capability.Registry,
		ReusableTokens: func(ctx context.Context) (bool, error) {
			settings, err := capability.Policy.ReadSettings(ctx)
			return settings.ReusableTokens, err
		},
		ReusableTokensForMutation: capability.Policy.ReusableTokensForMutation,
		Mutate:                    gatewayaccess.MutationRunner(component.observation.observationMutationRunner(handle, "user", nil, 0)),
		AcquireExclusive:          capability.Delivery.AcquireExclusive,
		FinishTokenInvalidation:   ports.FinishTokenInvalidation,
	}, true
}
