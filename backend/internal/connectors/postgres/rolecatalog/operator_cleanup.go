package rolecatalog

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

// CleanupConfirmed requires a fresh, explicit operator snapshot and current
// authority before connecting. Unresolved intents must first be reconciled by
// exact remote presence; absence never authorizes replay of cleanup DDL.
func CleanupConfirmed(ctx context.Context, journal *rolejournal.Journal, expected rolejournal.Entry, authority rolejournal.Anchor, dial Dial) (rolejournal.Entry, error) {
	if ctx == nil {
		return rolejournal.Entry{}, errors.New("managed Postgres cleanup context is unavailable")
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
	if expected.Record.Status != rolejournal.Provisioned {
		return rolejournal.Entry{}, rolejournal.ErrReconciliationRequired
	}
	return Cleanup(ctx, journal, expected, dial)
}
