// Package rolecatalog owns PostgreSQL managed-role catalog transactions.
package rolecatalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/cleanup"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/jackc/pgx/v5"
)

type transactionStarter interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// These shared catalogs fence role/database identity and markers. Dependency
// locks constrain new writers, but do not freeze an ownership/ACL snapshot:
// some dependency helpers release their locks before their transaction commits.
// Cleanup must still rely on PostgreSQL object locks/final dependency checks.
// NOWAIT rejects conflicting held locks; there is no unlocked fallback.
const catalogLock = `LOCK TABLE
	pg_catalog.pg_authid,
	pg_catalog.pg_auth_members,
	pg_catalog.pg_database,
	pg_catalog.pg_tablespace,
	pg_catalog.pg_shdescription,
	pg_catalog.pg_shdepend
IN SHARE ROW EXCLUSIVE MODE NOWAIT`

// Begin acquires the full catalog fence before reading any remote identity.
// Callers retain the returned transaction through identity verification, remote
// mutation and COMMIT. They must also hold local workspace lifecycle exclusion.
func Begin(ctx context.Context, connection transactionStarter) (pgx.Tx, error) {
	if resourcecontract.IsNilDependency(connection) {
		return nil, errors.New("managed Postgres catalog connection is unavailable")
	}
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		return nil, fmt.Errorf("begin managed Postgres catalog transaction: %w", err)
	}
	if resourcecontract.IsNilDependency(tx) {
		return nil, errors.New("managed Postgres catalog transaction is unavailable")
	}
	if _, err := tx.Exec(ctx, catalogLock); err != nil {
		rollbackErr := cleanup.Run(ctx, tx.Rollback)
		return nil, errors.Join(fmt.Errorf("managed Postgres catalog fence unavailable; verify catalog privileges and concurrent activity: %w", err), rollbackErr)
	}
	return tx, nil
}
