package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/history"
)

const approvalContextSchemaVersion = 1

func (s *Server) approvalContextDrift(ctx context.Context, runtime *databaseRuntime, request commandRequestRecord, command string) (bool, string, error) {
	var storedPayload string
	var storedHash string
	err := runtime.database.QueryRowContext(ctx, `
		SELECT approval_context, approval_context_hash
		FROM command_requests
		WHERE id = ?`,
		request.ID,
	).Scan(&storedPayload, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return true, "approval request no longer exists", nil
	}
	if err != nil {
		return false, "", err
	}
	if storedPayload == "" || storedHash == "" {
		return false, "", nil
	}
	return true, "approval context belongs to the retired command-request permission model; ask the AI to send a fresh connector action request", nil
}

func (s *Server) markCommandRequestStale(ctx context.Context, runtime *databaseRuntime, id int64, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'stale', approval_context_drift = ?, error = ?, completed_at = ?
		WHERE id = ? AND status = 'pending_approval'`,
		reason,
		reason,
		now,
		id,
	)
	if err != nil {
		return err
	}
	if err := requireAffected(result); err != nil {
		return err
	}
	return history.NewStore(runtime.database).SyncCommandRequest(ctx, id)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func expired(value string, now time.Time) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return !parsed.After(now)
}
