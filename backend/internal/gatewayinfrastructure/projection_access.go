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
	owner, ok := component.resolve(handle)
	if !ok || factory == nil {
		return false, gatewayaccess.ErrVaultMetadataAccessUnavailable
	}
	reader := factory.ForDatabase(owner.Storage.DatabaseHandle())
	if reader == nil {
		return false, gatewayaccess.ErrVaultMetadataAccessUnavailable
	}
	return reader.CanRead(ctx, tokenID, projectID, now)
}

func (component *AccessOwner) accessControlWorkspace(handle *WorkspaceHandle, ports AccessControlPorts) (gatewayaccess.AccessScope, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewayaccess.AccessScope{}, false
	}
	return gatewayaccess.AccessScope{
		Database: owner.Storage.DatabaseHandle(), Tokens: owner.Storage.TokenStore(),
		Registry: owner.Connectors.ConnectorRegistry(),
		ReusableTokens: func(ctx context.Context) (bool, error) {
			settings, err := owner.Security.PolicyService().ReadSettings(ctx)
			return settings.ReusableTokens, err
		},
		Mutate:                  gatewayaccess.MutationRunner(component.owner.observationMutationRunner(handle, "user", nil, 0)),
		AcquireExclusive:        owner.Security.VaultDeliveryCoordinator().AcquireExclusive,
		FinishTokenInvalidation: ports.FinishTokenInvalidation,
	}, true
}
