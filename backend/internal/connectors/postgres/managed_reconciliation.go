package postgresconnector

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

// ReconcileManagedRole requires a fresh operator-selected admin runtime and
// lifecycle exclusion. beforeConnect must persist the audit intent; errors
// prevent remote admission and journal confirmation.
func (Connector) ReconcileManagedRole(ctx context.Context, runtime connectors.RuntimeContext, expected rolejournal.Entry, beforeConnect func(context.Context) error) (rolejournal.Entry, error) {
	return managedRoleDecision(ctx, runtime, expected, beforeConnect, rolecatalog.Reconcile)
}

// CleanupManagedRole allows an explicitly confirmed operator decision to clean
// an exact provisioned journal identity, including one without a local profile.
// It does not publish, delete, or alter local credential profiles.
func (Connector) CleanupManagedRole(ctx context.Context, runtime connectors.RuntimeContext, expected rolejournal.Entry, beforeConnect func(context.Context) error) (rolejournal.Entry, error) {
	return managedRoleDecision(ctx, runtime, expected, beforeConnect, rolecatalog.CleanupConfirmed)
}

type managedRoleDecisionFunc func(context.Context, *rolejournal.Journal, rolejournal.Entry, rolejournal.Anchor, rolecatalog.Dial) (rolejournal.Entry, error)

func managedRoleDecision(ctx context.Context, runtime connectors.RuntimeContext, expected rolejournal.Entry, beforeConnect func(context.Context) error, decide managedRoleDecisionFunc) (rolejournal.Entry, error) {
	if runtime.Target.ConnectorKind != Kind || runtime.Profile.Kind != "username_password" || beforeConnect == nil {
		return rolejournal.Entry{}, errors.New("managed Postgres reconciliation runtime is unavailable")
	}
	journal, err := rolejournal.FromRuntime(runtime)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	authority, err := rolejournal.Authority(runtime)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	dial := managedRoleDial(runtime)
	return decide(ctx, journal, expected, authority, func(dialCtx context.Context) (rolecatalog.Connection, error) {
		if err := beforeConnect(dialCtx); err != nil {
			return nil, err
		}
		if err := dialCtx.Err(); err != nil {
			return nil, err
		}
		return dial(dialCtx)
	})
}
