package rolecatalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/cleanup"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/jackc/pgx/v5"
)

type Connection interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Close(context.Context) error
}

type Dial func(context.Context) (Connection, error)

const lifecycleTimeout = 20 * time.Second

func lifecycleTransaction(ctx context.Context, dial Dial) (Connection, pgx.Tx, error) {
	if ctx == nil {
		return nil, nil, errors.New("managed Postgres lifecycle context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if dial == nil {
		return nil, nil, errors.New("managed Postgres lifecycle connection is unavailable")
	}
	connection, err := dial(ctx)
	if err != nil {
		return nil, nil, err
	}
	if resourcecontract.IsNilDependency(connection) {
		return nil, nil, errors.New("managed Postgres lifecycle connection is unavailable")
	}
	tx, err := Begin(ctx, connection)
	if err != nil {
		closeConnection(ctx, connection)
		return nil, nil, err
	}
	return connection, tx, nil
}

func closeConnection(ctx context.Context, connection Connection) {
	_ = cleanup.Run(ctx, connection.Close)
}

func rollbackTransaction(ctx context.Context, tx pgx.Tx) error {
	return cleanup.Run(ctx, tx.Rollback)
}

type confirmation func(context.Context, rolejournal.Entry) (rolejournal.Entry, error)

func confirm(ctx context.Context, entry rolejournal.Entry, transition confirmation) (rolejournal.Entry, error) {
	confirmationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return transition(confirmationCtx, entry)
}

func uncertain(entry rolejournal.Entry, operation string, cause error) error {
	return connectors.ClassifyOutcomeUnknown(operation, map[string]any{
		"reconciliation_required": true, "journal_resource_id": entry.Reference().ResourceID,
		"journal_generation": entry.Record.Generation, "journal_status": string(entry.Record.Status),
	}, cause)
}

func failedTransaction(ctx context.Context, tx pgx.Tx, entry rolejournal.Entry, transition confirmation, cause error) error {
	if err := rollbackTransaction(ctx, tx); err != nil {
		return uncertain(entry, "transaction_rollback", errors.Join(cause, err))
	}
	if _, err := confirm(ctx, entry, transition); err != nil {
		return uncertain(entry, "rollback_confirmation", errors.Join(cause, err))
	}
	return cause
}

func executeStatements(ctx context.Context, tx pgx.Tx, statements []string) error {
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("execute managed Postgres role transaction: %w", err)
		}
	}
	return nil
}
