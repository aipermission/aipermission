package postgresconnector

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

func (Connector) CleanupProvisionedCredentialProfile(ctx context.Context, runtime connectors.RuntimeContext, profile connectors.CredentialProfileView) (connectors.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	if runtime.Target.ConnectorKind != Kind {
		return connectors.ActionResult{}, fmt.Errorf("target connector kind must be %s", Kind)
	}
	if !boolPublic(profile.Public, "managed_by_aipermission") {
		return connectors.ActionResult{Status: connectors.ResultCompleted, DisplayText: "No external cleanup required"}, nil
	}
	journal, entry, err := resolveManagedRole(ctx, runtime, profile)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	previouslyConfirmed := entry.Record.Status == rolejournal.Cleaned
	confirmed, err := rolecatalog.Cleanup(ctx, journal, entry, managedRoleDial(runtime))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return managedCleanupResult(confirmed, previouslyConfirmed), nil
}

func managedCleanupResult(confirmed rolejournal.Entry, previouslyConfirmed bool) connectors.ActionResult {
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"role_name": confirmed.Record.Intent.RoleName, "dropped": true, "ownership_reassigned": true,
			"ownership_reassigned_to":    confirmed.Record.Intent.Anchor.SuccessorName,
			"managed_privileges_removed": true, "previously_confirmed": previouslyConfirmed,
		},
		DisplayText: "Reassigned owned objects, revoked privileges and dropped managed Postgres role",
	}
}

func resolveManagedRole(ctx context.Context, runtime connectors.RuntimeContext, profile connectors.CredentialProfileView) (*rolejournal.Journal, rolejournal.Entry, error) {
	ref, err := managedRoleReference(runtime.Target, profile)
	if err != nil {
		return nil, rolejournal.Entry{}, err
	}
	if err := rolejournal.VerifyAuthority(runtime, ref.Intent.Anchor); err != nil {
		return nil, rolejournal.Entry{}, err
	}
	journal, err := rolejournal.FromRuntime(runtime)
	if err != nil {
		return nil, rolejournal.Entry{}, err
	}
	entry, err := journal.ResolveReference(ctx, ref)
	if err != nil {
		return nil, rolejournal.Entry{}, err
	}
	return journal, entry, nil
}

func managedRoleReference(target connectors.TargetView, profile connectors.CredentialProfileView) (rolejournal.Reference, error) {
	ref, err := rolejournal.ParseReference(profile.Public["managed_identity"])
	if err != nil {
		return rolejournal.Reference{}, errors.New("managed Postgres profile has no valid durable identity; operator adoption or manual cleanup is required")
	}
	// Compensation runs before a local profile ID exists. Target/admin and the
	// operation's immutable journal reference are sufficient; profile ID is not.
	if target.ConnectorKind != Kind || profile.TargetID != target.ID || profile.ConnectorKind != Kind ||
		profile.Public["username"] != ref.Intent.RoleName || profile.Public["managed_role_name"] != ref.Intent.RoleName ||
		int64Public(profile.Public, "managed_admin_profile_id") != ref.Intent.Anchor.AdminProfileID {
		return rolejournal.Reference{}, errors.New("managed Postgres credential does not match its recorded identity")
	}
	return ref, nil
}
