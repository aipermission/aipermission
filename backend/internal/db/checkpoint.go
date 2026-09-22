package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrDatabaseNotOpen = errors.New("database is not open")

// CheckpointForFilesystemMutation flushes every WAL frame before the caller
// renames, deletes, or closes database artifacts.
func CheckpointForFilesystemMutation(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return ErrDatabaseNotOpen
	}
	var busy, logFrames, checkpointedFrames int
	if err := database.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return fmt.Errorf("checkpoint database before filesystem mutation: %w", err)
	}
	if busy != 0 || checkpointedFrames < logFrames {
		return errors.New("checkpoint database before filesystem mutation: database remained busy")
	}
	return nil
}

// CheckpointFull asks SQLite to flush the current WAL without truncating it.
func CheckpointFull(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return ErrDatabaseNotOpen
	}
	return checkpointFullQuery(ctx, database)
}

type checkpointQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func checkpointFullConnection(ctx context.Context, connection *sql.Conn) error {
	if connection == nil {
		return ErrDatabaseNotOpen
	}
	return checkpointFullQuery(ctx, connection)
}

func checkpointFullQuery(ctx context.Context, query checkpointQuerier) error {
	var busy, logFrames, checkpointedFrames int
	if err := query.QueryRowContext(ctx, `PRAGMA wal_checkpoint(FULL)`).Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return fmt.Errorf("checkpoint database: %w", err)
	}
	if busy != 0 || checkpointedFrames < logFrames {
		return errors.New("checkpoint database: database remained busy")
	}
	return nil
}
