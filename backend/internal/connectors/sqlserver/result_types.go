package sqlserverconnector

import (
	"context"
	"database/sql"
	"fmt"
)

const describeResultSQL = `SELECT TOP (257) column_ordinal,system_type_id,error_number,error_type
 FROM sys.dm_exec_describe_first_result_set(@tsql,NULL,0) ORDER BY column_ordinal`

func unsupportedResultType() error {
	return fmt.Errorf("SQL Server result type is unsupported; CAST XML, sql_variant or CLR values to a supported scalar type before selecting them")
}

// Describe the exact compiled query on its connection before the driver can
// buffer unsupported LOBs. This assumes a trusted schema that does not change
// between describe and execute; it is not a hard heap limit for a hostile server.
func inspectResultTypes(ctx context.Context, conn *sql.Conn, query string) error {
	// A caller's row limit must never hide an unsafe later column.
	if _, err := conn.ExecContext(ctx, "SET ROWCOUNT 0"); err != nil {
		return err
	}
	rows, err := conn.QueryContext(ctx, describeResultSQL, sql.Named("tsql", query))
	if err != nil {
		return fmt.Errorf("describe SQL Server result before execution: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var ordinal, kind, errorNumber, errorType sql.NullInt64
		if err := rows.Scan(&ordinal, &kind, &errorNumber, &errorType); err != nil {
			return err
		}
		count++
		if errorNumber.Valid || errorType.Valid || !ordinal.Valid || ordinal.Int64 != int64(count) || count > 256 || !kind.Valid {
			return fmt.Errorf("SQL Server result metadata is unavailable or exceeds 256 columns; query was not executed")
		}
		if !supportedResultType(kind.Int64) {
			return unsupportedResultType()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("SQL Server result metadata is empty; query was not executed")
	}
	return rows.Close()
}

func supportedResultType(kind int64) bool {
	// SQL Server system_type_id, not the driver's wire type or user_type_id.
	// Scalar aliases resolve to these IDs; unknown/new types fail closed.
	switch kind {
	case 34, 35, 36, 40, 41, 42, 43, 48, 52, 56, 58, 59, 60, 61, 62,
		99, 104, 106, 108, 122, 127, 165, 167, 173, 175, 189, 231, 239:
		return true
	}
	return false
}
