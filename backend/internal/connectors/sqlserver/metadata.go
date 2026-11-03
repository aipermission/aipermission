package sqlserverconnector

import (
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const tablesSQL = `SELECT s.name AS table_schema, o.name AS table_name,
 CASE o.type WHEN 'V' THEN 'VIEW' ELSE 'BASE TABLE' END AS table_type
 FROM sys.objects AS o JOIN sys.schemas AS s ON s.schema_id=o.schema_id
 WHERE o.type IN ('U','V') AND o.is_ms_shipped=0 AND (@schema='' OR s.name=@schema)
 ORDER BY s.name,o.name`

const columnsSQL = `SELECT s.name AS table_schema,o.name AS table_name,c.column_id AS ordinal_position,
 c.name AS column_name,t.name AS data_type,c.max_length AS max_length_bytes,c.precision,c.scale,c.is_nullable,c.is_identity,
 CASE o.type WHEN 'V' THEN 'VIEW' ELSE 'BASE TABLE' END AS table_type
 FROM sys.objects AS o JOIN sys.schemas AS s ON s.schema_id=o.schema_id
 JOIN sys.columns AS c ON c.object_id=o.object_id
 JOIN sys.types AS t ON t.user_type_id=c.user_type_id
 WHERE o.type IN ('U','V') AND o.is_ms_shipped=0`

func actionQuery(action connectors.PreparedAction) (string, []any, int, error) {
	switch action.ActionName {
	case ActionGetSchemas:
		return `SELECT name AS schema_name FROM sys.schemas WHERE schema_id<16384 AND name NOT IN ('sys','INFORMATION_SCHEMA') ORDER BY name`, nil, maxRows, nil
	case ActionGetTables:
		return tablesSQL, []any{sql.Named("schema", text(action.Payload, "schema"))}, maxRows, nil
	case ActionDescribeTable:
		return columnsSQL + ` AND s.name=@schema AND o.name=@table ORDER BY c.column_id`, []any{sql.Named("schema", text(action.Payload, "schema")), sql.Named("table", text(action.Payload, "table"))}, maxRows, nil
	case ActionGetMetadata:
		return columnsSQL + ` ORDER BY s.name,o.name,c.column_id`, nil, maxMetadataRows, nil
	case ActionQueryReadonly:
		limit, ok := connectors.NativeIntValue(action.Payload["max_rows"])
		if !ok || limit < 1 || limit > maxRows {
			return "", nil, 0, fmt.Errorf("max_rows must be between 1 and %d", maxRows)
		}
		return text(action.Payload, "sql"), nil, limit, nil
	default:
		return "", nil, 0, fmt.Errorf("unsupported SQL Server action %q", action.ActionName)
	}
}
