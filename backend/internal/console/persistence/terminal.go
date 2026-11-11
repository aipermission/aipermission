// Package persistence owns durable terminal-session writes.
package persistence

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

const MaxChunkLength = 32768

func PersistTerminalStatus(ctx context.Context, database *sql.DB, sessionID int64, status, message, now string) error {
	_, err := database.ExecContext(ctx, `UPDATE console_sessions SET status = ?, error = ?, closed_at = COALESCE(closed_at, ?), updated_at = ? WHERE id = ?`, status, message, now, now, sessionID)
	return err
}

func PersistTranscript(ctx context.Context, database *sql.DB, sessionID int64, snapshot, pending, now string) error {
	err := transactionstate.Run(ctx, database, func(tx *sql.Tx) error {
		if pending != "" {
			nextSeq, err := nextChunkSequence(ctx, tx, sessionID)
			if err != nil {
				return err
			}
			for len(pending) > 0 {
				chunk := pending
				if len(chunk) > MaxChunkLength {
					chunk = pending[:MaxChunkLength]
				}
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO console_session_chunks (session_id, seq, data, created_at) VALUES (?, ?, ?, ?)`,
					sessionID, nextSeq, chunk, now,
				); err != nil {
					return fmt.Errorf("insert console transcript chunk: %w", err)
				}
				pending = pending[len(chunk):]
				nextSeq++
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE console_sessions SET transcript = ?, updated_at = ? WHERE id = ?`, snapshot, now, sessionID); err != nil {
			return fmt.Errorf("update console transcript snapshot: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist console transcript: %w", err)
	}
	return nil
}

func nextChunkSequence(ctx context.Context, tx *sql.Tx, sessionID int64) (int64, error) {
	var next sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM console_session_chunks WHERE session_id = ?`, sessionID).Scan(&next); err != nil {
		return 0, fmt.Errorf("read next console transcript chunk seq: %w", err)
	}
	if !next.Valid || next.Int64 < 1 {
		return 1, nil
	}
	return next.Int64, nil
}
