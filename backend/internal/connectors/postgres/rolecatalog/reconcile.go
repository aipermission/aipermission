package rolecatalog

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

// Reconcile verifies an unresolved bound role without repeating remote DDL.
// The caller must capture current authority and retain workspace lifecycle
// exclusion through this decision and any subsequent profile publication.
func Reconcile(ctx context.Context, journal *rolejournal.Journal, expected rolejournal.Entry, authority rolejournal.Anchor, dial Dial) (rolejournal.Entry, error) {
	if ctx == nil {
		return rolejournal.Entry{}, errors.New("managed Postgres reconciliation context is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, lifecycleTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return rolejournal.Entry{}, err
	}
	if journal == nil {
		return rolejournal.Entry{}, errors.New("managed Postgres role journal is unavailable")
	}
	if err := rolejournal.VerifyCurrentAuthority(authority, expected.Record.Intent.Anchor); err != nil {
		return rolejournal.Entry{}, err
	}
	if err := journal.ValidateCurrent(ctx, expected); err != nil {
		return rolejournal.Entry{}, err
	}
	transition, err := reconciliationTransition(journal, expected)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	connection, tx, err := lifecycleTransaction(ctx, dial)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	defer closeConnection(ctx, connection)
	defer rollbackTransaction(ctx, tx)
	if err := VerifyRole(ctx, tx, expected.Record); err != nil {
		return rolejournal.Entry{}, err
	}
	// Keep the fence until the journal has written and read back its decision.
	confirmed, err := transition(ctx, expected)
	if err != nil {
		return rolejournal.Entry{}, uncertain(expected, "role_reconciliation_confirmation", err)
	}
	return confirmed, nil
}

func reconciliationTransition(journal *rolejournal.Journal, entry rolejournal.Entry) (confirmation, error) {
	if entry.Record.RoleOID == 0 {
		return nil, rolejournal.ErrReconciliationRequired
	}
	switch entry.Record.Status {
	case rolejournal.ProvisionIntent:
		return journal.ConfirmProvision, nil
	case rolejournal.CleanupIntent:
		return journal.ConfirmCleanupNotApplied, nil
	default:
		return nil, rolejournal.ErrReconciliationRequired
	}
}
