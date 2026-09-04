package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const (
	historyRetentionDaysKey = "retention_history_days"
	auditRetentionDaysKey   = "retention_audit_days"
	consoleRetentionDaysKey = "retention_console_days"
	messageRetentionDaysKey = "retention_message_days"
)

type retentionSettingsResponse struct {
	HistoryDays int `json:"history_days"`
	AuditDays   int `json:"audit_days"`
	ConsoleDays int `json:"console_days"`
	MessageDays int `json:"message_days"`
}

type updateRetentionSettingsRequest retentionSettingsResponse

type purgeRetentionRequest struct {
	Target string `json:"target"`
	Days   int    `json:"days"`
}

type purgeRetentionResponse struct {
	Target  string `json:"target"`
	Days    int    `json:"days"`
	Deleted int64  `json:"deleted"`
}

func (s retentionHandlers) getRetentionSettings(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	settings, err := readRetentionSettings(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s retentionHandlers) updateRetentionSettings(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request updateRetentionSettingsRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	settings := retentionSettingsResponse(request)
	if err := validateRetentionSettings(settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	runtime.retentionMu.Lock()
	defer runtime.retentionMu.Unlock()
	deleted := map[string]int64{}
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "settings.retention.updated",
		func() any {
			return map[string]any{
				"history_days": settings.HistoryDays,
				"audit_days":   settings.AuditDays,
				"console_days": settings.ConsoleDays,
				"message_days": settings.MessageDays,
				"deleted":      deleted,
			}
		},
		func(tx *sql.Tx) error {
			if err := writeRetentionSettingsWithExecutor(r.Context(), tx, settings); err != nil {
				return err
			}
			var err error
			deleted, err = applyRetentionSettingsWithExecutor(r.Context(), tx, settings)
			return err
		},
	)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s retentionHandlers) purgeRetention(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request purgeRetentionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Days < 1 {
		writeError(w, http.StatusBadRequest, "days must be at least 1")
		return
	}
	runtime.retentionMu.Lock()
	defer runtime.retentionMu.Unlock()
	var deleted int64
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "settings.retention.purged",
		func() any { return map[string]any{"target": request.Target, "days": request.Days, "deleted": deleted} },
		func(tx *sql.Tx) error {
			var err error
			deleted, err = purgeRetentionTargetWithExecutor(r.Context(), tx, request.Target, request.Days)
			return err
		},
	)
	if err != nil {
		var invalid errInvalidQuery
		if errors.As(err, &invalid) {
			writeError(w, http.StatusBadRequest, invalid.Error())
			return
		}
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, purgeRetentionResponse{Target: request.Target, Days: request.Days, Deleted: deleted})
}

func readRetentionSettings(ctx context.Context, runtime *databaseRuntime) (retentionSettingsResponse, error) {
	values, err := readSettingsMap(ctx, runtime, historyRetentionDaysKey, auditRetentionDaysKey, consoleRetentionDaysKey, messageRetentionDaysKey)
	if err != nil {
		return retentionSettingsResponse{}, err
	}
	return retentionSettingsResponse{
		HistoryDays: parseRetentionDays(values[historyRetentionDaysKey]),
		AuditDays:   parseRetentionDays(values[auditRetentionDaysKey]),
		ConsoleDays: parseRetentionDays(values[consoleRetentionDaysKey]),
		MessageDays: parseRetentionDays(values[messageRetentionDaysKey]),
	}, nil
}

func writeRetentionSettings(ctx context.Context, runtime *databaseRuntime, settings retentionSettingsResponse) error {
	return writeRetentionSettingsWithExecutor(ctx, runtime.database, settings)
}

func writeRetentionSettingsWithExecutor(ctx context.Context, executor sqldb.Executor, settings retentionSettingsResponse) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range map[string]int{
		historyRetentionDaysKey: settings.HistoryDays,
		auditRetentionDaysKey:   settings.AuditDays,
		consoleRetentionDaysKey: settings.ConsoleDays,
		messageRetentionDaysKey: settings.MessageDays,
	} {
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			key,
			strconv.Itoa(value),
			now,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateRetentionSettings(settings retentionSettingsResponse) error {
	for _, value := range []int{settings.HistoryDays, settings.AuditDays, settings.ConsoleDays, settings.MessageDays} {
		if value < 0 {
			return errInvalidQuery("retention days cannot be negative")
		}
	}
	return nil
}

func applyRetentionSettings(ctx context.Context, runtime *databaseRuntime, settings retentionSettingsResponse) (map[string]int64, error) {
	executor, commit, rollback, err := sqldb.Transaction(ctx, runtime.database, nil, "retention settings")
	if err != nil {
		return nil, err
	}
	defer rollback()
	deleted, err := applyRetentionSettingsWithExecutor(ctx, executor, settings)
	if err != nil {
		return nil, err
	}
	if err := commit(); err != nil {
		return nil, fmt.Errorf("commit retention settings: %w", err)
	}
	return deleted, nil
}

func applyRetentionSettingsWithExecutor(ctx context.Context, executor sqldb.Executor, settings retentionSettingsResponse) (map[string]int64, error) {
	deleted := map[string]int64{}
	for target, days := range map[string]int{
		"history":  settings.HistoryDays,
		"audit":    settings.AuditDays,
		"console":  settings.ConsoleDays,
		"messages": settings.MessageDays,
	} {
		if days == 0 {
			continue
		}
		count, err := purgeRetentionTargetWithExecutor(ctx, executor, target, days)
		if err != nil {
			return nil, err
		}
		deleted[target] = count
	}
	count, err := purgeExpiredConnectorActionIdempotencyTombstones(ctx, executor)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		deleted["idempotency"] = count
	}
	return deleted, nil
}

func purgeRetentionTarget(ctx context.Context, runtime *databaseRuntime, target string, days int) (int64, error) {
	executor, commit, rollback, err := sqldb.Transaction(ctx, runtime.database, nil, "retention purge")
	if err != nil {
		return 0, err
	}
	defer rollback()
	deleted, err := purgeRetentionTargetWithExecutor(ctx, executor, target, days)
	if err != nil {
		return 0, err
	}
	if err := commit(); err != nil {
		return 0, fmt.Errorf("commit retention purge: %w", err)
	}
	return deleted, nil
}

func purgeRetentionTargetWithExecutor(ctx context.Context, executor sqldb.Executor, target string, days int) (int64, error) {
	switch target {
	case "history":
		return purgeHistoryRetentionWithExecutor(ctx, executor, days)
	case "audit":
		cutoff := "-" + strconv.Itoa(days) + " days"
		deleted, err := execRetentionDeleteWithCutoff(ctx, executor, `DELETE FROM audit_logs WHERE julianday(created_at) < julianday('now', ?)`, cutoff)
		if err != nil {
			return 0, err
		}
		_, err = execRetentionDeleteWithCutoff(ctx, executor, `
			DELETE FROM audit_outbox
			WHERE (delivered_at IS NOT NULL AND julianday(delivered_at) < julianday('now', ?))
				OR (dead_lettered_at IS NOT NULL AND julianday(dead_lettered_at) < julianday('now', ?))`, cutoff, cutoff)
		if err != nil {
			return 0, err
		}
		return deleted, nil
	case "console":
		return execRetentionDeleteWithCutoff(ctx, executor, `DELETE FROM console_sessions WHERE closed_at IS NOT NULL AND julianday(closed_at) < julianday('now', ?)`, "-"+strconv.Itoa(days)+" days")
	case "messages":
		return execRetentionDeleteWithCutoff(ctx, executor, `DELETE FROM message_queue WHERE consumed_at IS NOT NULL AND julianday(consumed_at) < julianday('now', ?)`, "-"+strconv.Itoa(days)+" days")
	default:
		return 0, errInvalidQuery("invalid retention target")
	}
}

func purgeHistoryRetention(ctx context.Context, runtime *databaseRuntime, days int) (int64, error) {
	return purgeRetentionTarget(ctx, runtime, "history", days)
}

func purgeHistoryRetentionWithExecutor(ctx context.Context, executor sqldb.Executor, days int) (int64, error) {
	cutoff := "-" + strconv.Itoa(days) + " days"
	total := int64(0)
	for _, statement := range []string{
		`DELETE FROM command_requests WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM connector_action_requests WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM file_transfer_batches WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM history_entries WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
	} {
		deleted, err := execRetentionDeleteWithCutoff(ctx, executor, statement, cutoff)
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
	deleted, err = purgeExpiredConnectorActionIdempotencyTombstones(ctx, executor)
	if err != nil {
		return 0, err
	}
	total += deleted
	return total, nil
}

func purgeExpiredConnectorActionIdempotencyTombstones(ctx context.Context, executor sqldb.Executor) (int64, error) {
	return execRetentionDeleteWithCutoff(ctx, executor,
		`DELETE FROM connector_action_idempotency_tombstones WHERE julianday(expires_at) <= julianday('now')`)
}

// Retention emits one audited summary for the purge transaction. The normal
// delete trigger remains authoritative for individual user-driven removals.
func purgeFileTransfersWithoutPerRowAudit(ctx context.Context, executor sqldb.Executor, cutoff string) (int64, error) {
	var outboxWatermark int64
	if err := executor.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM audit_outbox`).Scan(&outboxWatermark); err != nil {
		return 0, fmt.Errorf("read audit outbox watermark: %w", err)
	}
	deleted, err := execRetentionDeleteWithCutoff(ctx, executor,
		`DELETE FROM file_transfers WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`, cutoff)
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

func execRetentionDelete(ctx context.Context, runtime *databaseRuntime, statement string, days int) (int64, error) {
	return execRetentionDeleteWithCutoff(ctx, runtime.database, statement, "-"+strconv.Itoa(days)+" days")
}

func execRetentionDeleteWithCutoff(ctx context.Context, executor interface {
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
	return result.RowsAffected()
}

func parseRetentionDays(value string) int {
	days, err := strconv.Atoi(value)
	if err != nil || days < 0 {
		return 0
	}
	return days
}
