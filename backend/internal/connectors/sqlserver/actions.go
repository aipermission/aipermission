package sqlserverconnector

import (
	"context"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	ActionGetSchemas    = "get_schemas"
	ActionGetTables     = "get_tables"
	ActionDescribeTable = "describe_table"
	ActionGetMetadata   = "get_metadata"
	ActionQueryReadonly = "query_readonly"
	defaultMaxRows      = 100
	maxRows             = 1000
	maxMetadataRows     = 5000
	maxOutputBytes      = 500000
	maxCellBytes        = 64000
)

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{Title: Label + " target: " + target.Name, Connector: Label, ConnectorID: Kind,
		Summary:  "Inspect SQL Server metadata and run bounded read-only T-SQL using SQL authentication over direct TCP.",
		Usage:    []string{"Use get_schemas, get_tables and describe_table before querying unknown data.", "Use SELECT TOP (100) and schema-qualified tables such as dbo.orders. Only one SELECT or WITH SELECT is supported.", "get_metadata supplies ordered columns for autocomplete; truncated output is not a complete catalog."},
		Warnings: []string{"Use a dedicated least-privilege read-only SQL login, not sa or db_owner. Validation is defense in depth, not a substitute for database permissions.", "Writes, EXEC, procedures, SELECT INTO, sequences, external/linked-server access, table functions, hints and non-allowlisted functions are unsupported.", "Catalog actions enumerate the target database; permitted SQL can still expose server metadata visible to the login. Queries have deadlines, row, cell and output limits. TLS certificate verification is enabled by default.", "SQL Server 2012+ first-result inspection is required. CAST XML, sql_variant and CLR/spatial values to supported scalar types. Projected output limits are not a universal driver heap cap.", "Redaction is best-effort; do not intentionally query secrets."},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	schema := connectors.Field{Name: "schema", Label: "Schema", Type: connectors.FieldString, PreserveWhitespace: true}
	return []connectors.ActionDefinition{
		readAction(ActionGetSchemas, "List schemas", "List visible SQL Server schemas.", nil),
		readAction(ActionGetTables, "List tables", "List visible tables and views in the configured database.", []connectors.Field{schema}),
		readAction(ActionDescribeTable, "Describe table", "Inspect columns in database ordinal order.", []connectors.Field{schema, {Name: "table", Label: "Table", Type: connectors.FieldString, Required: true, PreserveWhitespace: true}}),
		readAction(ActionGetMetadata, "Load metadata", "Load bounded table/column metadata for the configured database.", nil),
		readAction(ActionQueryReadonly, "Run read-only query", "Run one bounded SELECT or WITH SELECT; no writes or procedures.", []connectors.Field{
			{Name: "sql", Label: "SQL", Type: connectors.FieldMultiline, Required: true, PreserveWhitespace: true},
			{Name: "max_rows", Label: "Max rows", Type: connectors.FieldInteger, Default: defaultMaxRows},
		}),
	}, nil
}

func readAction(name, label, description string, fields []connectors.Field) connectors.ActionDefinition {
	limit := maxRows
	if name == ActionGetMetadata {
		limit = maxMetadataRows
	}
	return connectors.ActionDefinition{Name: name, Label: label, Description: description, Category: "query", Risk: connectors.RiskRead,
		InputSchema: connectors.Schema{Fields: fields}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: limit, MaxBytes: maxOutputBytes}}
}

func (Connector) PrepareAction(ctx context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	if err := ctx.Err(); err != nil {
		return connectors.PreparedAction{}, err
	}
	definitions, _ := New().GetActionList(ctx, req.Target, req.Profile)
	for _, definition := range definitions {
		if definition.Name != req.ActionName {
			continue
		}
		payload, err := connectors.NormalizeSchemaValues(definition.InputSchema, req.Input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		if req.ActionName == ActionQueryReadonly {
			if err := validateReadonlySQL(text(payload, "sql")); err != nil {
				return connectors.PreparedAction{}, err
			}
			limit := payload["max_rows"].(int64)
			if limit < 1 || limit > maxRows {
				return connectors.PreparedAction{}, fmt.Errorf("max_rows must be between 1 and %d", maxRows)
			}
		}
		for _, key := range []string{"schema", "table"} {
			if value := text(payload, key); value != "" {
				if _, err := identifier(value); err != nil {
					return connectors.PreparedAction{}, err
				}
			}
		}
		if req.ActionName == ActionDescribeTable && text(payload, "schema") == "" {
			payload["schema"] = "dbo"
		}
		return connectors.PreparedAction{ConnectorKind: Kind, TargetRef: req.Target.Ref, ProfileID: req.Profile.ID, ActionName: req.ActionName, Risk: connectors.RiskRead,
			Title: definition.Label, Summary: definition.Description, Preview: payload, Payload: payload,
			ContextMaterial: map[string]any{"target_ref": req.Target.Ref, "profile_id": req.Profile.ID, "action_name": req.ActionName}}, nil
	}
	return connectors.PreparedAction{}, fmt.Errorf("unsupported SQL Server action %q", req.ActionName)
}
