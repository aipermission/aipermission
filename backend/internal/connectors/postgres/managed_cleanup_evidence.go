package postgresconnector

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

var _ connectors.ProvisionedCredentialCleanupEvidence = Connector{}

// ReadCompletedCredentialCleanup grants only local retirement after acknowledged
// cleanup. It cannot connect, decrypt the old admin, or infer success from absence.
func (Connector) ReadCompletedCredentialCleanup(ctx context.Context, evidence connectors.CleanupEvidenceContext, profile connectors.CredentialProfileView) (*connectors.ActionResult, error) {
	if ctx == nil {
		return nil, errors.New("managed Postgres cleanup evidence context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !boolPublic(profile.Public, "managed_by_aipermission") {
		return nil, nil
	}
	ref, err := managedRoleReference(evidence.Target, profile)
	if err != nil {
		return nil, err
	}
	if err := rolejournal.VerifyTargetAuthority(evidence.Target, ref.Intent.Anchor); err != nil {
		return nil, err
	}
	reader, err := rolejournal.CleanupEvidenceFrom(evidence.Capabilities)
	if err != nil {
		return nil, err
	}
	confirmed, completed, err := reader.ConfirmedCleanup(ctx, ref)
	if err != nil || !completed {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := managedCleanupResult(confirmed, true)
	return &result, nil
}
