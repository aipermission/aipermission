package sqlserverconnector

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
	mssql "github.com/microsoft/go-mssqldb"
)

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if err := validateRuntime(runtime); err != nil {
		return connectors.ActionResult{}, err
	}
	if action.ConnectorKind != Kind || action.TargetRef != runtime.Target.Ref || action.ProfileID != runtime.Profile.ID {
		return connectors.ActionResult{}, fmt.Errorf("SQL Server action identity does not match the current profile")
	}
	prepared, err := New().PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: action.ActionName, Input: action.Payload})
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if action.Risk != prepared.Risk {
		return connectors.ActionResult{}, fmt.Errorf("SQL Server action risk does not match the current action")
	}
	if prepared.ActionName == ActionExecuteSQL {
		return executeSQL(ctx, runtime, prepared)
	}
	query, args, limit, err := actionQuery(prepared)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if prepared.ActionName == ActionQueryReadonly {
		query, err = compileReadonlySQL(query)
		if err != nil {
			return connectors.ActionResult{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	db, err := connection(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer db.Close()
	started := time.Now()
	result, err := queryRows(ctx, db, query, limit, prepared.ActionName == ActionQueryReadonly, args...)
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return connectors.ActionResult{}, err
	}
	extras := map[string]any{"duration_ms": time.Since(started).Milliseconds(), "database": text(runtime.Target.Config, "database")}
	result = result.Fit(maxOutputBytes, extras)
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: result.ToMap(maxOutputBytes, extras), DisplayText: result.DisplayText()}, nil
}

func queryRows(ctx context.Context, db *sql.DB, query string, limit int, inspect bool, args ...any) (sqlresult.Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return sqlresult.Result{}, err
	}
	defer conn.Close()
	if inspect {
		if err := inspectResultTypes(ctx, conn, query); err != nil {
			return sqlresult.Result{}, err
		}
	}
	if _, err = conn.ExecContext(ctx, "SET ROWCOUNT "+strconv.Itoa(limit+1)); err != nil {
		return sqlresult.Result{}, err
	}
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return sqlresult.Result{}, fmt.Errorf("query SQL Server: %w", err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return sqlresult.Result{}, err
	}
	if len(types) > 256 {
		return sqlresult.Result{}, fmt.Errorf("SQL Server results may contain at most 256 columns")
	}
	columns := make([]string, len(types))
	for i, column := range types {
		switch strings.ToUpper(column.DatabaseTypeName()) {
		case "XML", "SQL_VARIANT", "UDT":
			return sqlresult.Result{}, unsupportedResultType()
		}
		columns[i] = column.Name()
	}
	builder := sqlresult.NewBuilder(columns, limit, maxOutputBytes, maxCellBytes, "...[truncated]")
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return sqlresult.Result{}, err
		}
		for i, value := range values {
			values[i] = normalizeValue(value, types[i].DatabaseTypeName())
		}
		if !builder.Add(values, func(value any) any { return value }) {
			cancel()
			return builder.Result(nil), nil
		}
	}
	if err := rows.Err(); err != nil {
		return sqlresult.Result{}, err
	}
	return builder.Result(nil), nil
}

func normalizeValue(value any, kind string) any {
	switch v := value.(type) {
	case []byte:
		if v == nil {
			return nil
		}
		switch strings.ToUpper(kind) {
		case "DECIMAL", "NUMERIC", "MONEY", "SMALLMONEY":
			return string(v)
		case "UNIQUEIDENTIFIER":
			var id mssql.UniqueIdentifier
			if id.Scan(v) == nil {
				return id.String()
			}
		}
		// Retain one extra byte so the result builder reports truncation without
		// allocating a hex string proportional to a potentially large value.
		return "0x" + hex.EncodeToString(v[:min(len(v), maxCellBytes/2+1)])
	case int64:
		if v > 9007199254740991 || v < -9007199254740991 {
			return strconv.FormatInt(v, 10)
		}
	case time.Time:
		switch strings.ToUpper(kind) {
		case "DATE":
			return v.Format("2006-01-02")
		case "TIME":
			return v.Format("15:04:05.999999999")
		case "DATETIME", "SMALLDATETIME", "DATETIME2":
			return v.Format("2006-01-02T15:04:05.999999999")
		default:
			return v.Format(time.RFC3339Nano)
		}
	}
	return value
}
