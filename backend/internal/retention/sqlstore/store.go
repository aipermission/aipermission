// Package sqlstore owns the SQLite persistence adapter for retention policy.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type Store struct{}

func (Store) ReadSettings(ctx context.Context, database *sql.DB, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		var value string
		err := database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func (Store) WriteSetting(ctx context.Context, executor sqldb.Executor, key, value, updatedAt string) error {
	_, err := executor.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, updatedAt,
	)
	return err
}

func (Store) PurgeHistory(ctx context.Context, executor sqldb.Executor, cutoff string) (int64, error) {
	var total int64
	for _, statement := range []string{
		`DELETE FROM command_requests WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM connector_action_requests WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM profile_restore_operations WHERE completed_at IS NOT NULL AND audit_pending = 0 AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM file_transfer_batches WHERE completed_at IS NOT NULL AND archive_path = '' AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM history_entries WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
	} {
		deleted, err := deleteWithCutoff(ctx, executor, statement, cutoff)
		if err != nil {
			return 0, err
		}
		total += deleted
	}
	deleted, err := purgeFileTransfersWithoutPerRowAudit(ctx, executor, cutoff)
	if err != nil {
		return 0, err
	}
	total += deleted
	deleted, err = purgeExpiredConnectorActionIdempotency(ctx, executor)
	if err != nil {
		return 0, err
	}
	total += deleted
	deleted, err = purgeExpiredFileTransferIdempotency(ctx, executor)
	if err != nil {
		return 0, err
	}
	total += deleted
	deleted, err = purgeExpiredProfileRestoreIdempotency(ctx, executor)
	return total + deleted, err
}

func (Store) PurgeAudit(ctx context.Context, executor sqldb.Executor, cutoff string) (int64, error) {
	deleted, err := deleteWithCutoff(ctx, executor, `DELETE FROM audit_logs WHERE julianday(created_at) < julianday('now', ?)`, cutoff)
	if err != nil {
		return 0, err
	}
	_, err = deleteWithCutoff(ctx, executor, `
		DELETE FROM audit_outbox
		WHERE (delivered_at IS NOT NULL AND julianday(delivered_at) < julianday('now', ?))
			OR (dead_lettered_at IS NOT NULL AND julianday(dead_lettered_at) < julianday('now', ?))`, cutoff, cutoff)
	return deleted, err
}

func (Store) PurgeConsole(ctx context.Context, executor sqldb.Executor, cutoff string) (int64, error) {
	return deleteWithCutoff(ctx, executor, `DELETE FROM console_sessions WHERE closed_at IS NOT NULL AND julianday(closed_at) < julianday('now', ?)`, cutoff)
}

func (Store) PurgeMessages(ctx context.Context, executor sqldb.Executor, cutoff string) (int64, error) {
	return deleteWithCutoff(ctx, executor, `DELETE FROM message_queue WHERE consumed_at IS NOT NULL AND julianday(consumed_at) < julianday('now', ?)`, cutoff)
}

func (Store) PurgeExpiredIdempotency(ctx context.Context, executor sqldb.Executor) (int64, error) {
	connectorActions, err := purgeExpiredConnectorActionIdempotency(ctx, executor)
	if err != nil {
		return 0, err
	}
	fileTransfers, err := purgeExpiredFileTransferIdempotency(ctx, executor)
	if err != nil {
		return 0, err
	}
	profileRestores, err := purgeExpiredProfileRestoreIdempotency(ctx, executor)
	return connectorActions + fileTransfers + profileRestores, err
}

func purgeExpiredConnectorActionIdempotency(ctx context.Context, executor sqldb.Executor) (int64, error) {
	return deleteWithCutoff(ctx, executor, `DELETE FROM connector_action_idempotency_tombstones WHERE julianday(expires_at) <= julianday('now')`)
}

func purgeExpiredFileTransferIdempotency(ctx context.Context, executor sqldb.Executor) (int64, error) {
	return deleteWithCutoff(ctx, executor, `DELETE FROM file_transfer_start_idempotency WHERE julianday(expires_at) <= julianday('now')`)
}

func purgeExpiredProfileRestoreIdempotency(ctx context.Context, executor sqldb.Executor) (int64, error) {
	return deleteWithCutoff(ctx, executor, `DELETE FROM profile_restore_idempotency_tombstones WHERE julianday(expires_at) <= julianday('now')`)
}

// Retention emits one audited summary for the purge transaction. The normal
// trigger remains authoritative for individual user-driven removals.
func purgeFileTransfersWithoutPerRowAudit(ctx context.Context, executor sqldb.Executor, cutoff string) (int64, error) {
	var outboxWatermark int64
	if err := executor.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM audit_outbox`).Scan(&outboxWatermark); err != nil {
		return 0, fmt.Errorf("read audit outbox watermark: %w", err)
	}
	deleted, err := deleteWithCutoff(ctx, executor,
		`DELETE FROM file_transfers
		 WHERE completed_at IS NOT NULL
		   AND remote_staging_ref = ''
		   AND temp_path = ''
		   AND julianday(completed_at) < julianday('now', ?)`, cutoff)
	if err != nil {
		return 0, err
	}
	if _, err := executor.ExecContext(ctx, `
		DELETE FROM audit_outbox
		WHERE id > ? AND actor_type = 'gateway' AND action = 'file_transfer.removed'`, outboxWatermark); err != nil {
		return 0, fmt.Errorf("remove retention-generated file transfer audit events: %w", err)
	}
	return deleted, nil
}

func deleteWithCutoff(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, statement string, cutoffs ...string) (int64, error) {
	arguments := make([]any, len(cutoffs))
	for index, cutoff := range cutoffs {
		arguments[index] = cutoff
	}
	result, err := executor.ExecContext(ctx, statement, arguments...)
	if err != nil {
		return 0, err
	}
	return sqldb.RowsAffected(result, "purge retained records")
}
