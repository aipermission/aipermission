package rolecatalog

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var sharedOwnershipLocks = [...]string{
	`SET LOCAL lock_timeout = '5s'`,
	`SELECT oid FROM pg_catalog.pg_database ORDER BY oid FOR UPDATE`,
	`SELECT oid FROM pg_catalog.pg_tablespace ORDER BY oid FOR UPDATE`,
}

// The relation fence prevents invisible new shared objects. Complete row scans
// additionally drain prior ALTER OWNER updates whose relation locks were already
// released. The scope query MUST be a later READ COMMITTED statement, not part of
// this scan's snapshot. No LIMIT/SKIP LOCKED or unlocked fallback is safe here.
func lockSharedOwnershipRows(ctx context.Context, tx pgx.Tx) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, statement := range sharedOwnershipLocks {
		// pgx Exec consumes the complete SELECT result before returning.
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("drain managed Postgres shared ownership writers: %w", err)
		}
	}
	return nil
}
