package sqldb

import (
	"database/sql"
	"errors"
	"fmt"
)

// RowsAffected reads a mutation count without silently discarding driver
// errors. The operation label keeps failures actionable at package boundaries.
func RowsAffected(result sql.Result, operation string) (int64, error) {
	if result == nil {
		return 0, errors.New("read affected rows: SQL result is nil")
	}
	affected, err := result.RowsAffected()
	if err == nil {
		return affected, nil
	}
	if operation == "" {
		return 0, fmt.Errorf("read affected rows: %w", err)
	}
	return 0, fmt.Errorf("read affected rows for %s: %w", operation, err)
}
