// Package retention owns workspace data-retention settings, cleanup
// transactions, and the periodic cleanup lifecycle.
package retention

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type settingValue struct {
	key   string
	value string
}

const (
	historyDaysKey = "retention_history_days"
	auditDaysKey   = "retention_audit_days"
	consoleDaysKey = "retention_console_days"
	messageDaysKey = "retention_message_days"
)

type Settings struct {
	HistoryDays int `json:"history_days"`
	AuditDays   int `json:"audit_days"`
	ConsoleDays int `json:"console_days"`
	MessageDays int `json:"message_days"`
}

type ValidationError string

func (e ValidationError) Error() string { return string(e) }

func ValidateSettings(settings Settings) error {
	for _, value := range []int{settings.HistoryDays, settings.AuditDays, settings.ConsoleDays, settings.MessageDays} {
		if value < 0 {
			return ValidationError("retention days cannot be negative")
		}
	}
	return nil
}

func readSettings(ctx context.Context, database *sql.DB, store repository) (Settings, error) {
	values, err := store.ReadSettings(ctx, database, settingKeys())
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		HistoryDays: parseDays(values[historyDaysKey]),
		AuditDays:   parseDays(values[auditDaysKey]),
		ConsoleDays: parseDays(values[consoleDaysKey]),
		MessageDays: parseDays(values[messageDaysKey]),
	}, nil
}

func settingKeys() []string {
	return []string{historyDaysKey, auditDaysKey, consoleDaysKey, messageDaysKey}
}

func writeSettings(ctx context.Context, executor sqldb.Executor, store repository, settings Settings) error {
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	for _, setting := range settingValues(settings) {
		if err := store.WriteSetting(ctx, executor, setting.key, setting.value, updatedAt); err != nil {
			return err
		}
	}
	return nil
}

func settingValues(settings Settings) []settingValue {
	return []settingValue{
		{key: historyDaysKey, value: strconv.Itoa(settings.HistoryDays)},
		{key: auditDaysKey, value: strconv.Itoa(settings.AuditDays)},
		{key: consoleDaysKey, value: strconv.Itoa(settings.ConsoleDays)},
		{key: messageDaysKey, value: strconv.Itoa(settings.MessageDays)},
	}
}

func parseDays(value string) int {
	days, err := strconv.Atoi(value)
	if err != nil || days < 0 {
		return 0
	}
	return days
}
