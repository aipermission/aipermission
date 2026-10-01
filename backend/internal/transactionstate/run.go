package transactionstate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
)

// Run owns a pinned local transaction. Uncertain physical connections are
// retired before any caller can reconcile through the database's pool.
func Run(ctx context.Context, database *sql.DB, mutate func(*sql.Tx) error) error {
	if ctx == nil || database == nil || mutate == nil {
		return NotCommitted(errors.New("local transaction dependencies are unavailable"))
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return NotCommitted(fmt.Errorf("acquire local transaction connection: %w", err))
	}
	defer connection.Close()
	if err := ctx.Err(); err != nil {
		return NotCommitted(err)
	}
	// Keep transaction finalization synchronous with this owner. Passing the
	// caller's cancelable context lets database/sql close this pinned connection
	// asynchronously while retirement is acquiring it. Callback SQL still uses
	// the original context; admission and publication check cancellation below.
	tx, err := connection.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		discardConnection(connection)
		return NotCommitted(fmt.Errorf("begin local transaction: %w", err))
	}
	finalized := false
	defer func() {
		if !finalized {
			if err := tx.Rollback(); err != nil {
				discardConnection(connection)
			}
		}
	}()
	err = ctx.Err()
	if err == nil {
		err = mutate(tx)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		rollbackErr := tx.Rollback()
		finalized = true
		if rollbackErr != nil {
			discardConnection(connection)
			return UnknownWithSafeReadback(errors.Join(err, fmt.Errorf("rollback local transaction: %w", rollbackErr)))
		}
		return NotCommitted(err)
	}
	err = tx.Commit()
	finalized = true
	if err != nil {
		discardConnection(connection)
		return UnknownWithSafeReadback(fmt.Errorf("commit local transaction: %w", err))
	}
	// Deferred Close releases the pinned slot before Run returns to a caller
	// that may project audit or read back through the same single-connection pool.
	return nil
}

func discardConnection(connection *sql.Conn) {
	// A driver COMMIT failure can leave its transaction open after sql.Tx is
	// done. Raw/ErrBadConn removes this physical connection from the pool.
	_ = connection.Raw(func(any) error { return driver.ErrBadConn })
}
