package timestampmigration

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/timeformat"
)

const migrationBatchSize = 256

type columnSpec struct {
	table   string
	columns []string
}

var chronologicalColumns = []columnSpec{
	{table: "history_entries", columns: []string{"created_at", "started_at", "completed_at", "updated_at"}},
	{table: "audit_logs", columns: []string{"created_at"}},
	{table: "audit_outbox", columns: []string{"occurred_at", "created_at", "delivered_at", "last_attempt_at", "next_attempt_at", "dead_lettered_at"}},
	{table: "audit_dispatch_state", columns: []string{"last_failure_at", "last_success_at", "updated_at"}},
	{table: "command_requests", columns: []string{"created_at", "completed_at"}},
	{table: "connector_action_requests", columns: []string{"created_at", "dispatch_started_at", "execution_lease_expires_at", "completed_at"}},
	{table: "vault_action_requests", columns: []string{"created_at", "expires_at", "completed_at", "updated_at"}},
	{table: "console_sessions", columns: []string{"created_at", "updated_at", "closed_at"}},
	{table: "file_transfers", columns: []string{"created_at", "started_at", "completed_at", "updated_at"}},
	{table: "file_transfer_batches", columns: []string{"created_at", "started_at", "completed_at", "updated_at"}},
	{table: "backup_records", columns: []string{"backup_created_at", "uploaded_at", "created_at", "updated_at", "deleted_at"}},
}

func Normalize(tx *sql.Tx) error {
	for _, spec := range chronologicalColumns {
		if err := normalizeColumns(tx, spec); err != nil {
			return err
		}
	}
	return canonicalizeAuditTriggers(tx)
}

func canonicalizeAuditTriggers(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT name, sql FROM sqlite_master
		WHERE type = 'trigger' AND name LIKE 'audit_%' AND sql LIKE '%strftime(''%fZ''%'
		ORDER BY name`)
	if err != nil {
		return fmt.Errorf("list audit timestamp triggers: %w", err)
	}
	type trigger struct{ name, statement string }
	triggers := []trigger{}
	for rows.Next() {
		var item trigger
		if err := rows.Scan(&item.name, &item.statement); err != nil {
			rows.Close()
			return fmt.Errorf("scan audit timestamp trigger: %w", err)
		}
		triggers = append(triggers, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate audit timestamp triggers: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close audit timestamp trigger rows: %w", err)
	}
	for _, item := range triggers {
		quotedName := `"` + strings.ReplaceAll(item.name, `"`, `""`) + `"`
		if _, err := tx.Exec(`DROP TRIGGER ` + quotedName); err != nil {
			return fmt.Errorf("drop audit timestamp trigger %s: %w", item.name, err)
		}
		statement := strings.ReplaceAll(item.statement, "%fZ", "%f000000Z")
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("recreate audit timestamp trigger %s: %w", item.name, err)
		}
	}
	return nil
}

func normalizeColumns(tx *sql.Tx, spec columnSpec) error {
	query := "SELECT id"
	for _, column := range spec.columns {
		query += ", " + column
	}
	query += " FROM " + spec.table + " WHERE id > ? ORDER BY id LIMIT ?"
	type update struct {
		id     int64
		values []sql.NullString
	}
	statement := "UPDATE " + spec.table + " SET "
	for index, column := range spec.columns {
		if index > 0 {
			statement += ", "
		}
		statement += column + " = ?"
	}
	statement += " WHERE id = ?"
	lastID := int64(math.MinInt64)
	for {
		rows, err := tx.Query(query, lastID, migrationBatchSize)
		if err != nil {
			return fmt.Errorf("read %s timestamps: %w", spec.table, err)
		}
		updates := make([]update, 0, migrationBatchSize)
		for rows.Next() {
			item := update{values: make([]sql.NullString, len(spec.columns))}
			destinations := make([]any, 0, len(spec.columns)+1)
			destinations = append(destinations, &item.id)
			for index := range item.values {
				destinations = append(destinations, &item.values[index])
			}
			if err := rows.Scan(destinations...); err != nil {
				rows.Close()
				return fmt.Errorf("scan %s timestamps: %w", spec.table, err)
			}
			for index, value := range item.values {
				if !value.Valid || value.String == "" {
					continue
				}
				parsed, err := parseStored(value.String)
				if err != nil {
					rows.Close()
					return fmt.Errorf("parse %s.%s timestamp for row %d: %w", spec.table, spec.columns[index], item.id, err)
				}
				item.values[index].String = timeformat.UTC(parsed)
			}
			updates = append(updates, item)
			lastID = item.id
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate %s timestamps: %w", spec.table, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close %s timestamps: %w", spec.table, err)
		}
		for _, item := range updates {
			arguments := make([]any, 0, len(item.values)+1)
			for _, value := range item.values {
				if value.Valid {
					arguments = append(arguments, value.String)
				} else {
					arguments = append(arguments, nil)
				}
			}
			arguments = append(arguments, item.id)
			if _, err := tx.Exec(statement, arguments...); err != nil {
				return fmt.Errorf("normalize %s timestamps: %w", spec.table, err)
			}
		}
		if len(updates) < migrationBatchSize {
			return nil
		}
	}
}

func parseStored(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp %q", value)
}
