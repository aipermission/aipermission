package rolecatalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

// Cleanup requires a freshly resolved immutable credential reference and a
// separate current-authority check, even when a Cleaned record skips dispatch.
func Cleanup(ctx context.Context, journal *rolejournal.Journal, entry rolejournal.Entry, dial Dial) (rolejournal.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, lifecycleTimeout)
	defer cancel()
	if journal == nil {
		return rolejournal.Entry{}, errors.New("managed Postgres role journal is unavailable")
	}
	if entry.Record.Status == rolejournal.Cleaned {
		confirmed, _, err := journal.BeginCleanup(ctx, entry)
		return confirmed, err
	}
	if entry.Record.Status != rolejournal.Provisioned {
		return rolejournal.Entry{}, rolejournal.ErrReconciliationRequired
	}
	connection, tx, err := lifecycleTransaction(ctx, dial)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	defer closeConnection(ctx, connection)
	defer rollbackTransaction(ctx, tx)
	plan, err := CleanupPlan(ctx, tx, entry.Record)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	intent, dispatch, err := journal.BeginCleanup(ctx, entry)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	if !dispatch {
		return rolejournal.Entry{}, errors.New("managed Postgres cleanup dispatch was not authorized")
	}
	if err := executeStatements(ctx, tx, plan); err != nil {
		return rolejournal.Entry{}, failedTransaction(ctx, tx, intent, journal.ConfirmCleanupRollback, err)
	}
	if err := tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			if _, confirmationErr := confirm(ctx, intent, journal.ConfirmCleanupRollback); confirmationErr != nil {
				return rolejournal.Entry{}, uncertain(intent, "rollback_confirmation", errors.Join(err, confirmationErr))
			}
			return rolejournal.Entry{}, fmt.Errorf("managed Postgres cleanup transaction rolled back at commit: %w", err)
		}
		// Name absence cannot prove that ownership and privileges were handled.
		return rolejournal.Entry{}, uncertain(intent, "transaction_commit", err)
	}
	confirmed, err := confirm(ctx, intent, journal.ConfirmCleanup)
	if err != nil {
		return rolejournal.Entry{}, uncertain(intent, "cleanup_confirmation", err)
	}
	return confirmed, nil
}
