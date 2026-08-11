package postgresconnector

import (
	"context"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
	"github.com/jackc/pgx/v5"
)

func getTables(ctx context.Context, tx pgx.Tx, schema string, includeSystem bool) (queryOutput, error) {
	query := `
		SELECT table_schema, table_name, table_type
		FROM information_schema.tables
		WHERE ($1 = '' OR table_schema = $1)`
	if !includeSystem {
		query += ` AND table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema NOT LIKE 'pg_toast%'`
	}
	query += ` ORDER BY table_schema, table_name`
	return queryRows(ctx, tx, query, 1000, schema)
}

func describeTable(ctx context.Context, tx pgx.Tx, schema string, table string) (queryOutput, error) {
	query := `
		SELECT table_schema, table_name, ordinal_position, column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_name = $1
			AND ($2 = '' OR table_schema = $2)
			AND table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name, ordinal_position`
	return queryRows(ctx, tx, query, 500, table, schema)
}

func queryRows(ctx context.Context, tx pgx.Tx, sql string, rowLimit int, args ...any) (queryOutput, error) {
	if rowLimit < 1 {
		rowLimit = defaultMaxRows
	}
	if rowLimit > maxRows {
		rowLimit = maxRows
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return queryOutput{}, fmt.Errorf("query postgres: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		columns = append(columns, field.Name)
	}
	builder := sqlresult.NewBuilder(columns, rowLimit, maxOutputBytes, maxCellBytes, truncatedSuffix)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return queryOutput{}, fmt.Errorf("read postgres row: %w", err)
		}
		if !builder.Add(values, normalizePostgresValue) {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return queryOutput{}, fmt.Errorf("iterate postgres rows: %w", err)
	}
	return queryOutput{Result: builder.Result(nil)}, nil
}

func normalizePostgresValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return typed
	}
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	cut := 0
	for index := range value {
		if index > limit {
			break
		}
		cut = index
	}
	if cut == 0 {
		return ""
	}
	return value[:cut]
}
