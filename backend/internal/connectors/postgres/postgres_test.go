package postgresconnector

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestConnectorMetadataAndSchemas(t *testing.T) {
	connector := New()
	if connector.Kind() != Kind || connector.Label() != Label || connector.Version() == "" {
		t.Fatalf("unexpected metadata kind=%q label=%q version=%q", connector.Kind(), connector.Label(), connector.Version())
	}

	targetSchema := connector.TargetSchema()
	if !hasField(targetSchema, "connection_mode") || !hasField(targetSchema, "host") || !hasField(targetSchema, "database") {
		t.Fatalf("expected connection_mode, host, and database target fields, got %#v", targetSchema.Fields)
	}

	credentialSchemas := connector.CredentialSchemas()
	if len(credentialSchemas) != 1 || credentialSchemas[0].Kind != "username_password" {
		t.Fatalf("unexpected credential schemas: %#v", credentialSchemas)
	}
	if !hasField(credentialSchemas[0].Schema, "username") || !hasField(credentialSchemas[0].Schema, "password") {
		t.Fatalf("expected username and password credential fields, got %#v", credentialSchemas[0].Schema.Fields)
	}
}

func TestGetHelpAndActionList(t *testing.T) {
	connector := New()
	target := connectors.TargetView{Ref: "postgres:7:11", Name: "main-db", ConnectorKind: Kind}

	help, err := connector.GetHelp(context.Background(), target)
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if help.ConnectorID != Kind || !strings.Contains(help.Title, "main-db") || len(help.Usage) == 0 || len(help.Warnings) == 0 {
		t.Fatalf("unexpected help: %#v", help)
	}

	actions, err := connector.GetActionList(context.Background(), target)
	if err != nil {
		t.Fatalf("action list: %v", err)
	}
	if len(actions) != 4 {
		t.Fatalf("expected 4 actions, got %d", len(actions))
	}
	for index, want := range []string{ActionGetSchemas, ActionGetTables, ActionDescribeTable, ActionQueryReadonly} {
		if actions[index].Name != want || actions[index].Risk != connectors.RiskRead {
			t.Fatalf("action[%d] = %#v", index, actions[index])
		}
	}
}

func TestPrepareMetadataActions(t *testing.T) {
	connector := New()
	req := connectors.ActionRequest{
		Target:  connectors.TargetView{Ref: "postgres:7:11", Name: "main-db", ConnectorKind: Kind},
		Profile: connectors.CredentialProfileView{ID: 11, ConnectorKind: Kind, Kind: "username_password", Label: "readonly"},
	}

	schemas, err := connector.PrepareAction(context.Background(), withAction(req, ActionGetSchemas, nil))
	if err != nil {
		t.Fatalf("prepare get_schemas: %v", err)
	}
	if schemas.ActionName != ActionGetSchemas || schemas.ProfileID != 11 || schemas.Risk != connectors.RiskRead {
		t.Fatalf("unexpected schemas action: %#v", schemas)
	}

	tables, err := connector.PrepareAction(context.Background(), withAction(req, ActionGetTables, map[string]any{
		"schema":         " public ",
		"include_system": "true",
	}))
	if err != nil {
		t.Fatalf("prepare get_tables: %v", err)
	}
	if tables.Payload["schema"] != "public" || tables.Payload["include_system"] != true {
		t.Fatalf("unexpected tables payload: %#v", tables.Payload)
	}
}

func TestPrepareDescribeTable(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "postgres:7:11", Name: "main-db", ConnectorKind: Kind},
		Profile:    connectors.CredentialProfileView{ID: 11, ConnectorKind: Kind},
		ActionName: ActionDescribeTable,
		Input: map[string]any{
			"schema": "public",
			"table":  "orders",
		},
	})
	if err != nil {
		t.Fatalf("prepare describe_table: %v", err)
	}
	if prepared.Payload["schema"] != "public" || prepared.Payload["table"] != "orders" {
		t.Fatalf("unexpected payload: %#v", prepared.Payload)
	}
}

func TestPrepareDescribeTableRequiresTable(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "postgres:7:11", ConnectorKind: Kind},
		ActionName: ActionDescribeTable,
		Input:      map[string]any{"table": " "},
	})
	if err == nil {
		t.Fatal("expected missing table error")
	}
}

func TestPrepareReadonlyQuery(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "postgres:7:11", Name: "main-db", ConnectorKind: Kind},
		Profile:    connectors.CredentialProfileView{ID: 11, ConnectorKind: Kind},
		ActionName: ActionQueryReadonly,
		Input: map[string]any{
			"sql":      "-- smoke\nselect id, email from users where active = true;",
			"max_rows": float64(maxRows + 500),
		},
		Reason: "inspect active users",
	})
	if err != nil {
		t.Fatalf("prepare query_readonly: %v", err)
	}
	if prepared.Payload["max_rows"] != maxRows {
		t.Fatalf("max_rows = %#v", prepared.Payload["max_rows"])
	}
	if prepared.ContextMaterial["reason"] != "inspect active users" {
		t.Fatalf("reason missing from context material: %#v", prepared.ContextMaterial)
	}
}

func TestPrepareReadonlyQueryRejectsUnsafeSQL(t *testing.T) {
	for _, sql := range []string{
		"",
		"update users set admin = true",
		"select 1; drop table users",
		"with deleted as (delete from users returning *) select * from deleted",
		"listen events",
	} {
		_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
			Target:     connectors.TargetView{Ref: "postgres:7:11", ConnectorKind: Kind},
			ActionName: ActionQueryReadonly,
			Input:      map[string]any{"sql": sql},
		})
		if err == nil {
			t.Fatalf("expected %q to be rejected", sql)
		}
	}
}

func TestPrepareUnsupportedAction(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "postgres:7:11", ConnectorKind: Kind},
		ActionName: "vacuum",
	})
	if !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("expected ErrUnsupportedAction, got %v", err)
	}
}

func TestExecuteActionNotWired(t *testing.T) {
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{}, connectors.PreparedAction{ActionName: ActionQueryReadonly})
	if !errors.Is(err, ErrExecutionNotWired) {
		t.Fatalf("expected ErrExecutionNotWired, got %v", err)
	}
}

func withAction(req connectors.ActionRequest, actionName string, input map[string]any) connectors.ActionRequest {
	req.ActionName = actionName
	req.Input = input
	return req
}

func hasField(schema connectors.Schema, name string) bool {
	for _, field := range schema.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}
