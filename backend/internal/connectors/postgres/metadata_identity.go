package postgresconnector

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5"
)

// Metadata names are parameter values, not SQL fragments or quoted SQL tokens.
func metadataIdentifierInput(input map[string]any, name string) (string, error) {
	raw := input[name]
	if raw == nil {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("%s must be an exact UTF-8 identifier without NUL", name)
	}
	return value, nil
}

func queryMetadata(ctx context.Context, tx pgx.Tx, action connectors.PreparedAction) (queryOutput, error) {
	schema, err := metadataIdentifierInput(action.Payload, "schema")
	if err != nil {
		return queryOutput{}, err
	}
	if action.ActionName == ActionGetTables {
		return getTables(ctx, tx, schema, payloadBool(action.Payload, "include_system"))
	}
	table, err := metadataIdentifierInput(action.Payload, "table")
	if err != nil {
		return queryOutput{}, err
	}
	if table == "" {
		return queryOutput{}, fmt.Errorf("%s table is required", ActionDescribeTable)
	}
	return describeTable(ctx, tx, schema, table)
}
