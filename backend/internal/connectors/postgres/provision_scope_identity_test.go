package postgresconnector

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5"
)

func exactProvisionScope(schema, table string, columns any) map[string]any {
	return map[string]any{"scope": map[string]any{"schemas": []any{map[string]any{
		"schema": schema, "tables": []any{map[string]any{"table": table, "columns": columns}},
	}}}}
}

func TestProvisionScopePreservesOpaqueResourceNamesThroughGrants(t *testing.T) {
	for _, names := range [][3]string{
		{" public ", " users ", " id "}, {`schema";--`, `table"name`, "column,name"},
		{" ", "\n", "\t"}, {"日本語", "table-name", "quoted'column"},
	} {
		for _, encoded := range []bool{false, true} {
			input := exactProvisionScope(names[0], names[1], []any{names[2]})
			if encoded {
				text, err := json.Marshal(input["scope"])
				if err != nil {
					t.Fatal(err)
				}
				input["scope"] = string(text)
			}
			scope, err := provisionScopeInput(input)
			if err != nil || len(scope.Schemas) != 1 || scope.Schemas[0].Schema != names[0] ||
				len(scope.Schemas[0].Tables) != 1 || scope.Schemas[0].Tables[0].Table != names[1] ||
				!reflect.DeepEqual(scope.Schemas[0].Tables[0].Columns, []string{names[2]}) {
				t.Fatalf("scope identity changed: %#v %v", scope, err)
			}
			statements, summary, err := provisionRoleStatements(connectors.TargetView{Config: map[string]any{"database": "appdb"}}, "reader", "test-password", "read_only", scope)
			if err != nil {
				t.Fatal(err)
			}
			want := "GRANT SELECT (" + pgx.Identifier{names[2]}.Sanitize() + ") ON TABLE " + pgx.Identifier{names[0], names[1]}.Sanitize() + ` TO "reader"`
			if statements[len(statements)-1] != want {
				t.Fatalf("grant targets a different or unquoted resource: %q want %q", statements, want)
			}
			grant := summary["grants"].([]map[string]any)[0]
			if grant["schema"] != names[0] || grant["table"] != names[1] || !reflect.DeepEqual(grant["columns"], []string{names[2]}) {
				t.Fatalf("grant summary changed identity: %#v", grant)
			}
		}
	}
}

func TestProvisionScopeRejectsInvalidOrTruncatingResourceNames(t *testing.T) {
	for _, value := range []any{nil, "", "a\x00b", string([]byte{0xff}), strings.Repeat("a", 64), strings.Repeat("é", 32), 7, false, []string{"id"}} {
		for _, field := range []string{"schema", "table", "column"} {
			if got, err := provisionScopeIdentifier(map[string]any{field: value}, field); err == nil || got != "" {
				t.Fatalf("invalid %s accepted: %q %v", field, got, err)
			}
		}
	}
	for _, columns := range []any{nil, "id,email", []any{}, []any{nil}, []any{"id", 7}, []any{"id", "bad\x00column"}} {
		if _, err := provisionScopeInput(exactProvisionScope("public", "users", columns)); err == nil {
			t.Fatalf("invalid column identities accepted: %#v", columns)
		}
	}
	name := strings.Repeat("é", 31) + "x"
	for _, columns := range []any{[]string{name}, []any{name}} {
		got, err := provisionScopeColumns(columns)
		if err != nil || !reflect.DeepEqual(got, []string{name}) {
			t.Fatalf("63-byte exact column rejected/changed: %#v %v", got, err)
		}
	}
}
