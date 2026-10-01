package rolecatalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

// Provision requires workspace lifecycle exclusion through caller publication.
// Intent and role binding must survive a restart before COMMIT can be sent.
func Provision(ctx context.Context, journal *rolejournal.Journal, authority rolejournal.Anchor, role string, statements []string, dial Dial) (rolejournal.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, lifecycleTimeout)
	defer cancel()
	if err := authority.ValidateAuthority(); err != nil {
		return rolejournal.Entry{}, err
	}
	if journal == nil || len(statements) == 0 {
		return rolejournal.Entry{}, errors.New("managed Postgres provisioning plan is unavailable")
	}
	connection, tx, err := lifecycleTransaction(ctx, dial)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	defer closeConnection(ctx, connection)
	defer rollbackTransaction(ctx, tx)
	anchor, err := CaptureAnchor(ctx, tx, authority)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	entry, err := journal.BeginProvision(ctx, anchor, role)
	if err != nil {
		return rolejournal.Entry{}, err
	}
	marker := "COMMENT ON ROLE " + pgx.Identifier{role}.Sanitize() + " IS '" + strings.ReplaceAll(entry.Record.Intent.Marker(), "'", "''") + "'"
	plan := append(append([]string(nil), statements...), marker)
	if err := executeStatements(ctx, tx, plan); err != nil {
		return rolejournal.Entry{}, failedTransaction(ctx, tx, entry, journal.ConfirmRollback, err)
	}
	oid, err := CreatedRoleOID(ctx, tx, entry)
	if err != nil {
		return rolejournal.Entry{}, failedTransaction(ctx, tx, entry, journal.ConfirmRollback, err)
	}
	bound, err := journal.BindRole(ctx, entry, oid)
	if err != nil {
		return rolejournal.Entry{}, failedTransaction(ctx, tx, entry, journal.ConfirmRollback, err)
	}
	if err := tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			if _, confirmationErr := confirm(ctx, bound, journal.ConfirmRollback); confirmationErr != nil {
				return rolejournal.Entry{}, uncertain(bound, "rollback_confirmation", errors.Join(err, confirmationErr))
			}
			return rolejournal.Entry{}, fmt.Errorf("managed Postgres role transaction rolled back at commit: %w", err)
		}
		// This is observation only: never repeat CREATE ROLE or grant statements.
		closeConnection(ctx, connection)
		verifyCtx, verifyCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer verifyCancel()
		confirmed, verifyErr := Reconcile(verifyCtx, journal, bound, authority, dial)
		if verifyErr != nil {
			return rolejournal.Entry{}, uncertain(bound, "transaction_commit", errors.Join(err, verifyErr))
		}
		return confirmed, nil
	}
	confirmed, err := confirm(ctx, bound, journal.ConfirmProvision)
	if err != nil {
		return rolejournal.Entry{}, uncertain(bound, "provision_confirmation", err)
	}
	return confirmed, nil
}
