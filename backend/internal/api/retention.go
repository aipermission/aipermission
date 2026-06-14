package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"
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
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	settings := retentionSettingsResponse(request)
	if err := validateRetentionSettings(settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := writeRetentionSettings(r.Context(), runtime, settings); err != nil {
		writeInternalError(w)
		return
	}
	deleted, err := applyRetentionSettings(r.Context(), runtime, settings)
	if err != nil {
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "settings.retention.updated", map[string]any{
		"history_days": settings.HistoryDays,
		"audit_days":   settings.AuditDays,
		"console_days": settings.ConsoleDays,
		"message_days": settings.MessageDays,
		"deleted":      deleted,
	})
	writeJSON(w, http.StatusOK, settings)
}

func (s retentionHandlers) purgeRetention(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request purgeRetentionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if request.Days < 1 {
		writeError(w, http.StatusBadRequest, "days must be at least 1")
		return
	}
	deleted, err := purgeRetentionTarget(r.Context(), runtime, request.Target, request.Days)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "settings.retention.purged", map[string]any{
		"target":  request.Target,
		"days":    request.Days,
		"deleted": deleted,
	})
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
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range map[string]int{
		historyRetentionDaysKey: settings.HistoryDays,
		auditRetentionDaysKey:   settings.AuditDays,
		consoleRetentionDaysKey: settings.ConsoleDays,
		messageRetentionDaysKey: settings.MessageDays,
	} {
		if _, err := runtime.database.ExecContext(ctx, `
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
		count, err := purgeRetentionTarget(ctx, runtime, target, days)
		if err != nil {
			return nil, err
		}
		deleted[target] = count
	}
	return deleted, nil
}

func purgeRetentionTarget(ctx context.Context, runtime *databaseRuntime, target string, days int) (int64, error) {
	switch target {
	case "history":
		return purgeHistoryRetention(ctx, runtime, days)
	case "audit":
		return execRetentionDelete(ctx, runtime, `DELETE FROM audit_logs WHERE julianday(created_at) < julianday('now', ?)`, days)
	case "console":
		return execRetentionDelete(ctx, runtime, `DELETE FROM console_sessions WHERE closed_at IS NOT NULL AND julianday(closed_at) < julianday('now', ?)`, days)
	case "messages":
		return execRetentionDelete(ctx, runtime, `DELETE FROM message_queue WHERE consumed_at IS NOT NULL AND julianday(consumed_at) < julianday('now', ?)`, days)
	default:
		return 0, errInvalidQuery("invalid retention target")
	}
}

func purgeHistoryRetention(ctx context.Context, runtime *databaseRuntime, days int) (int64, error) {
	tx, err := runtime.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	cutoff := "-" + strconv.Itoa(days) + " days"
	total := int64(0)
	for _, statement := range []string{
		`DELETE FROM command_requests WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM connector_action_requests WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM file_transfers WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM file_transfer_batches WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
		`DELETE FROM history_entries WHERE completed_at IS NOT NULL AND julianday(completed_at) < julianday('now', ?)`,
	} {
		deleted, err := execRetentionDeleteWithCutoff(ctx, tx, statement, cutoff)
		if err != nil {
			return 0, err
		}
		total += deleted
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

func execRetentionDelete(ctx context.Context, runtime *databaseRuntime, statement string, days int) (int64, error) {
	return execRetentionDeleteWithCutoff(ctx, runtime.database, statement, "-"+strconv.Itoa(days)+" days")
}

func execRetentionDeleteWithCutoff(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, statement string, cutoff string) (int64, error) {
	result, err := executor.ExecContext(ctx, statement, cutoff)
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
