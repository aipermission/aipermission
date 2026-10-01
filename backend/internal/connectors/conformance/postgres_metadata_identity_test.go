package conformance_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/jackc/pgx/v5"
)

func assertPostgresExactMetadata(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	conn := connectPostgresPolicyFixture(t)
	defer conn.Close(context.Background())
	base := "ap_meta_" + rand.Text()
	schema, table, column := " "+base+" ", " table\";\n ", " column\";\n "
	for _, name := range []string{base, schema} {
		quoted := pgx.Identifier{name}.Sanitize()
		if _, err := conn.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
			t.Fatal(err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := conn.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
				t.Errorf("remove owned metadata fixture: %v", err)
			}
		}()
		statement := "CREATE TABLE " + pgx.Identifier{name, table}.Sanitize() + " (" + pgx.Identifier{column}.Sanitize() + " integer, id integer)"
		if _, err := conn.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{base, schema} {
		listed := executeAction(t, connector, runtime, postgresconnector.ActionGetTables, map[string]any{"schema": name})
		rows := postgresMetadataFixtureRows(t, listed)
		if len(rows) != 1 || rows[0]["table_schema"] != name || rows[0]["table_name"] != table {
			t.Fatalf("metadata filter changed schema identity: %#v", rows)
		}
		described := executeAction(t, connector, runtime, postgresconnector.ActionDescribeTable, map[string]any{"schema": name, "table": table})
		columns := postgresMetadataFixtureRows(t, described)
		if len(columns) != 2 || columns[0]["column_name"] != column || columns[1]["column_name"] != "id" {
			t.Fatalf("metadata lost column identity/order: %#v", columns)
		}
		for _, row := range columns {
			if row["table_schema"] != name || row["table_name"] != table {
				t.Fatalf("metadata selection reached a sibling table: %#v", row)
			}
		}
	}
}

func postgresMetadataFixtureRows(t *testing.T, result connectors.ActionResult) []map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Rows      []map[string]any `json:"rows"`
		Truncated bool             `json:"truncated"`
	}
	if err := json.Unmarshal(encoded, &output); err != nil || output.Truncated {
		t.Fatalf("invalid metadata fixture output: %v truncated=%v", err, output.Truncated)
	}
	return output.Rows
}
