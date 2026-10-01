package postgresconnector

import (
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestPostgresMetadataNamesRemainExact(t *testing.T) {
	catalog, err := New().GetActionList(t.Context(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"public", " public ", " ", `"quoted"`, "quotes\";\n\r", "özel şema"} {
		for _, action := range catalog {
			if action.Name != ActionGetTables && action.Name != ActionDescribeTable {
				continue
			}
			input := map[string]any{"schema": name}
			if action.Name == ActionDescribeTable {
				input["table"] = name
			}
			normalized, err := connectors.NormalizeSchemaValues(action.InputSchema, input)
			if err != nil {
				t.Fatalf("exact metadata name rejected by catalog: %v", err)
			}
			prepared, err := New().PrepareAction(t.Context(), connectors.ActionRequest{ActionName: action.Name, Input: normalized})
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range input {
				if prepared.Payload[key] != value || prepared.Preview[key] != value || prepared.ContextMaterial[key] != value {
					t.Fatalf("%s/%s identity changed: payload=%#v preview=%#v context=%#v", action.Name, key, prepared.Payload, prepared.Preview, prepared.ContextMaterial)
				}
			}
		}
	}
}

func TestPostgresInvalidMetadataNameCannotBecomeWildcard(t *testing.T) {
	for _, value := range []any{"schema\x00suffix", string([]byte{0xff}), 123, true, []string{"public"}} {
		for _, action := range []string{ActionGetTables, ActionDescribeTable} {
			for _, field := range []string{"schema", "table"} {
				if field == "table" && action == ActionGetTables {
					continue
				}
				input := map[string]any{"schema": "public", "table": "orders"}
				input[field] = value
				prepared, err := New().PrepareAction(t.Context(), connectors.ActionRequest{ActionName: action, Input: input})
				if err == nil || prepared.Payload != nil {
					t.Fatalf("invalid %s name became a dispatched action: %#v", field, prepared)
				}
			}
		}
	}
}

func TestPostgresMetadataWildcardAndMissingTable(t *testing.T) {
	for _, input := range []map[string]any{nil, {"schema": nil}, {"schema": ""}} {
		prepared, err := New().PrepareAction(t.Context(), connectors.ActionRequest{ActionName: ActionGetTables, Input: input})
		if err != nil || prepared.Payload["schema"] != "" {
			t.Fatalf("omitted optional schema changed: %#v err=%v", prepared, err)
		}
	}
	for _, input := range []map[string]any{nil, {"table": nil}, {"table": ""}} {
		if _, err := New().PrepareAction(t.Context(), connectors.ActionRequest{ActionName: ActionDescribeTable, Input: input}); err == nil {
			t.Fatal("missing table was accepted")
		}
	}
}

func TestPostgresMetadataBindsExactNames(t *testing.T) {
	schema, table := " schema\";\n ", " table\";\n "
	getTx := &queryValueTransaction{rows: &queryValueRows{}}
	if _, err := getTables(t.Context(), getTx, schema, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(getTx.arguments, []any{schema}) {
		t.Fatal("table listing changed the schema before dispatch")
	}
	describeTx := &queryValueTransaction{rows: &queryValueRows{}}
	if _, err := describeTable(t.Context(), describeTx, schema, table); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(describeTx.arguments, []any{table, schema}) {
		t.Fatal("column listing changed a resource name before dispatch")
	}
}

func TestPostgresPreparedMetadataDispatchPreservesExactNames(t *testing.T) {
	for _, name := range []string{" ", " schema\";\n ", "özel şema"} {
		for _, action := range []string{ActionGetTables, ActionDescribeTable} {
			prepared, err := New().PrepareAction(t.Context(), connectors.ActionRequest{
				ActionName: action, Input: map[string]any{"schema": name, "table": name},
			})
			if err != nil {
				t.Fatal(err)
			}
			tx := &queryValueTransaction{rows: &queryValueRows{}}
			if _, err := queryMetadata(t.Context(), tx, prepared); err != nil {
				t.Fatal(err)
			}
			want := []any{name}
			if action == ActionDescribeTable {
				want = append(want, name)
			}
			if !reflect.DeepEqual(tx.arguments, want) {
				t.Fatalf("prepared %s dispatch changed identity: %#v, want %#v", action, tx.arguments, want)
			}
		}
	}
}

func TestPostgresMetadataDispatchRejectsMalformedPreparedNames(t *testing.T) {
	for _, payload := range []map[string]any{
		{"schema": 123, "table": "valid"}, {"schema": "public", "table": true},
		{"schema": "public", "table": ""}, {"schema": "bad\x00", "table": "valid"},
	} {
		tx := &queryValueTransaction{rows: &queryValueRows{}}
		_, err := queryMetadata(t.Context(), tx, connectors.PreparedAction{ActionName: ActionDescribeTable, Payload: payload})
		if err == nil || tx.arguments != nil {
			t.Fatalf("malformed prepared metadata reached SQL: %#v %v", tx.arguments, err)
		}
	}
}
