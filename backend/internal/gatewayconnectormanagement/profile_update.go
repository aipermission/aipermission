package gatewayconnectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func UpdatePreparedCredentialProfile(
	ctx context.Context,
	store *connectortargets.Store,
	target Target,
	profile CredentialProfile,
	prepared PreparedCredentialProfile,
	ports CredentialPreparationPorts,
	ensure func(context.Context, *connectortargets.Store, Target, CredentialProfile) error,
) (CredentialProfile, error) {
	var ensureDomain func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error
	if ensure != nil {
		ensureDomain = func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return ensure(ctx, store, targetFromDomain(target), credentialProfileFromDomain(profile))
		}
	}
	updated, err := connectormanagement.UpdatePreparedCredentialProfile(
		ctx, store, target.domain(), profile.domain(), connectormanagement.PreparedCredentialProfile(prepared), ports.domain(), ensureDomain,
	)
	return credentialProfileFromDomain(updated), err
}
