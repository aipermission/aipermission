package postgresconnector

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type provisionScope struct {
	AllSchemas bool
	Schemas    []provisionSchemaScope
}

type provisionSchemaScope struct {
	Schema    string
	AllTables bool
	Tables    []provisionTableScope
}

type provisionTableScope struct {
	Table      string
	AllColumns bool
	Columns    []string
}

func provisionScopeInput(input map[string]any) (provisionScope, error) {
	raw := input["scope"]
	if raw == nil {
		return provisionScope{AllSchemas: true}, nil
	}
	if text, ok := raw.(string); ok {
		if strings.TrimSpace(text) == "" {
			return provisionScope{AllSchemas: true}, nil
		}
		var decoded map[string]any
		if err := decodeProvisionScopeJSON(text, &decoded); err != nil {
			return provisionScope{}, fmt.Errorf("scope must be a JSON object")
		}
		raw = decoded
	}
	scopeMap, ok := raw.(map[string]any)
	if !ok {
		return provisionScope{}, fmt.Errorf("scope must be a JSON object")
	}
	scope := provisionScope{AllSchemas: boolInput(scopeMap, "all_schemas")}
	if scope.AllSchemas {
		return scope, nil
	}
	for _, item := range anySlice(scopeMap["schemas"]) {
		schemaMap, ok := item.(map[string]any)
		if !ok {
			return provisionScope{}, fmt.Errorf("scope schemas must be objects")
		}
		name, err := provisionScopeIdentifier(schemaMap, "schema")
		if err != nil {
			return provisionScope{}, err
		}
		schema := provisionSchemaScope{Schema: name, AllTables: boolInput(schemaMap, "all_tables")}
		if !schema.AllTables {
			for _, tableItem := range anySlice(schemaMap["tables"]) {
				tableMap, ok := tableItem.(map[string]any)
				if !ok {
					return provisionScope{}, fmt.Errorf("scope tables must be objects")
				}
				name, err := provisionScopeIdentifier(tableMap, "table")
				if err != nil {
					return provisionScope{}, err
				}
				table := provisionTableScope{Table: name, AllColumns: boolInput(tableMap, "all_columns")}
				if !table.AllColumns {
					table.Columns, err = provisionScopeColumns(tableMap["columns"])
					if err != nil {
						return provisionScope{}, err
					}
				}
				schema.Tables = append(schema.Tables, table)
			}
			if len(schema.Tables) == 0 {
				return provisionScope{}, fmt.Errorf("selected schema must grant all tables or at least one table")
			}
		}
		scope.Schemas = append(scope.Schemas, schema)
	}
	if len(scope.Schemas) == 0 {
		return provisionScope{}, fmt.Errorf("scope must include at least one schema or all_schemas=true")
	}
	return scope, nil
}

func provisionRoleStatements(target connectors.TargetView, roleName string, password string, preset string, scope provisionScope) ([]string, map[string]any, error) {
	database := targetString(target.Config, "database")
	if database == "" {
		return nil, nil, fmt.Errorf("target database is required")
	}
	roleSQL := quoteIdentifier(roleName)
	statements := []string{fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s", roleSQL, quoteLiteral(password))}
	statements = append(statements, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", quoteIdentifier(database), roleSQL))
	grants := []map[string]any{}
	privileges := "SELECT"
	if preset == "read_write" {
		privileges = "SELECT, INSERT, UPDATE, DELETE"
	}
	if scope.AllSchemas {
		grants = append(grants, map[string]any{"all_schemas": true, "all_tables": true, "privileges": privileges})
		statements = append(statements, provisionDOBlock(fmt.Sprintf(`
DECLARE schema_name text;
BEGIN
	FOR schema_name IN
		SELECT nspname FROM pg_namespace
		WHERE nspname NOT LIKE 'pg_%%' AND nspname <> 'information_schema'
	LOOP
		EXECUTE format('GRANT USAGE ON SCHEMA %%I TO %%I', schema_name, %s);
		EXECUTE format('GRANT %s ON ALL TABLES IN SCHEMA %%I TO %%I', schema_name, %s);
	END LOOP;
END
`, quoteLiteral(roleName), privileges, quoteLiteral(roleName))))
		if preset == "read_write" {
			statements = append(statements, provisionOwnedSequenceGrant(roleName, "ns.nspname NOT LIKE 'pg_%' AND ns.nspname <> 'information_schema'"))
		}
		return statements, provisionRoleSummary(preset, database, grants), nil
	}
	for _, schema := range scope.Schemas {
		schemaSQL := quoteIdentifier(schema.Schema)
		statements = append(statements, fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", schemaSQL, roleSQL))
		if schema.AllTables {
			statements = append(statements, fmt.Sprintf("GRANT %s ON ALL TABLES IN SCHEMA %s TO %s", privileges, schemaSQL, roleSQL))
			if preset == "read_write" {
				statements = append(statements, provisionOwnedSequenceGrant(roleName, "ns.nspname = "+quoteLiteral(schema.Schema)))
			}
			grants = append(grants, map[string]any{"schema": schema.Schema, "all_tables": true, "privileges": privileges})
			continue
		}
		for _, table := range schema.Tables {
			if !table.AllColumns && preset == "read_write" {
				return nil, nil, fmt.Errorf("column-scoped read/write grants are not supported; choose all columns for write access")
			}
			if table.AllColumns {
				statements = append(statements, fmt.Sprintf("GRANT %s ON TABLE %s TO %s", privileges, qualifiedIdentifierSQL(schema.Schema, table.Table), roleSQL))
				if preset == "read_write" {
					filter := "ns.nspname = " + quoteLiteral(schema.Schema) + " AND tbl.relname = " + quoteLiteral(table.Table)
					statements = append(statements, provisionOwnedSequenceGrant(roleName, filter))
				}
				grants = append(grants, map[string]any{"schema": schema.Schema, "table": table.Table, "all_columns": true, "privileges": privileges})
				continue
			}
			columnSQL := make([]string, 0, len(table.Columns))
			for _, column := range table.Columns {
				columnSQL = append(columnSQL, quoteIdentifier(column))
			}
			statements = append(statements, fmt.Sprintf("GRANT SELECT (%s) ON TABLE %s TO %s", strings.Join(columnSQL, ", "), qualifiedIdentifierSQL(schema.Schema, table.Table), roleSQL))
			grants = append(grants, map[string]any{"schema": schema.Schema, "table": table.Table, "columns": table.Columns, "privileges": "SELECT"})
		}
	}
	return statements, provisionRoleSummary(preset, database, grants), nil
}

func provisionOwnedSequenceGrant(roleName, tableFilter string) string {
	return provisionDOBlock(fmt.Sprintf(`
DECLARE sequence_oid oid;
BEGIN
	FOR sequence_oid IN
		SELECT DISTINCT to_regclass(owned.sequence_name)::oid
		FROM pg_catalog.pg_class tbl
		JOIN pg_catalog.pg_namespace ns ON ns.oid = tbl.relnamespace
		JOIN pg_catalog.pg_attribute att ON att.attrelid = tbl.oid
		CROSS JOIN LATERAL (
			SELECT pg_get_serial_sequence(format('%%I.%%I', ns.nspname, tbl.relname), att.attname) AS sequence_name
		) owned
		WHERE tbl.relkind IN ('r', 'p')
			AND att.attnum > 0
			AND NOT att.attisdropped
			AND owned.sequence_name IS NOT NULL
			AND %s
	LOOP
		EXECUTE format('GRANT USAGE ON SEQUENCE %%s TO %%I', sequence_oid::regclass, %s);
	END LOOP;
END
`, tableFilter, quoteLiteral(roleName)))
}

func provisionRoleSummary(preset, database string, grants []map[string]any) map[string]any {
	summary := map[string]any{"preset": preset, "database": database, "grants": grants}
	if preset == "read_write" {
		summary["sequence_privileges"] = "usage_on_sequences_owned_by_writable_tables"
	}
	return summary
}

func randomCredentialPassword() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func provisionRiskLabel(preset string) string {
	if preset == "read_write" {
		return "managed read-write"
	}
	return "managed read-only"
}

func boolPublic(public map[string]any, name string) bool {
	if public == nil {
		return false
	}
	value, ok := public[name]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
