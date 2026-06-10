// Package postgresconnector defines the Postgres connector contract.
package postgresconnector

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "postgres"
	Label   = "Postgres"
	Version = "0.1"

	ActionGetSchemas    = "get_schemas"
	ActionGetTables     = "get_tables"
	ActionDescribeTable = "describe_table"
	ActionQueryReadonly = "query_readonly"

	defaultMaxRows = 100
	maxRows        = 1000
	maxSQLBytes    = 20000
)

var (
	ErrUnsupportedAction = errors.New("unsupported postgres connector action")
	ErrExecutionNotWired = errors.New("postgres connector execution is not wired yet")

	disallowedReadonlyTerms = regexp.MustCompile(`\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|copy|call|do|vacuum|analyze|reindex|cluster|refresh|merge)\b`)
)

// Connector describes Postgres as a connector-shaped target. The first 0.2.0
// stage exposes metadata, action contracts, and preparation only; runtime
// execution is wired in a later commit.
type Connector struct{}

func New() Connector {
	return Connector{}
}

func (Connector) Kind() string {
	return Kind
}

func (Connector) Label() string {
	return Label
}

func (Connector) Version() string {
	return Version
}

func (Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{
			Name:        "connection_mode",
			Label:       "Connection mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "direct",
			Description: "Use direct for locally reachable databases. Over SSH will route through a configured SSH target.",
			Options: []connectors.FieldOption{
				{Value: "direct", Label: "Direct"},
				{Value: "over_ssh", Label: "Over SSH"},
			},
		},
		{
			Name:        "host",
			Label:       "Host",
			Type:        connectors.FieldString,
			Required:    true,
			Description: "Postgres host or service address as seen by the gateway.",
		},
		{
			Name:        "port",
			Label:       "Port",
			Type:        connectors.FieldNumber,
			Required:    true,
			Default:     5432,
			Description: "Postgres port.",
		},
		{
			Name:        "database",
			Label:       "Database",
			Type:        connectors.FieldString,
			Required:    true,
			Description: "Database name.",
		},
		{
			Name:        "ssl_mode",
			Label:       "SSL mode",
			Type:        connectors.FieldSelect,
			Default:     "prefer",
			Description: "Postgres SSL mode for direct connections.",
			Options: []connectors.FieldOption{
				{Value: "disable", Label: "Disable"},
				{Value: "prefer", Label: "Prefer"},
				{Value: "require", Label: "Require"},
				{Value: "verify_full", Label: "Verify full"},
			},
		},
		{
			Name:        "ssh_transport_target_ref",
			Label:       "SSH transport target",
			Type:        connectors.FieldString,
			Description: "Optional SSH target ref used when connection_mode is over_ssh.",
		},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:        "username_password",
			Label:       "Username and password",
			Description: "Postgres username and password stored through the encrypted vault layer.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "username",
					Label:       "Username",
					Type:        connectors.FieldString,
					Required:    true,
					Description: "Postgres role used for this credential profile.",
				},
				{
					Name:        "password",
					Label:       "Password",
					Type:        connectors.FieldSecret,
					Required:    true,
					Secret:      true,
					Description: "Postgres password for this profile.",
				},
			}},
		},
	}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	title := "Postgres target"
	if target.Name != "" {
		title = "Postgres target: " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Inspect Postgres metadata and run bounded read-only SQL through AIPermission approval rules.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use get_schemas and get_tables before composing SQL when the database shape is unknown.",
			"Use describe_table for columns before querying application data.",
			"Use query_readonly only for SELECT, WITH, SHOW, or EXPLAIN-style reads and include a short reason.",
			"Prefer small max_rows values and ask for approval before reading sensitive business data.",
		},
		Warnings: []string{
			"Postgres credential profiles decide what the database itself allows; prefer dedicated read-only roles.",
			"query_readonly is designed for reads, not migrations or writes.",
			"Redaction is best-effort. Do not intentionally query secrets unless the operator approved that access.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{
		{
			Name:        ActionGetSchemas,
			Label:       "List schemas",
			Description: "List visible Postgres schemas.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxRows: 200},
		},
		{
			Name:        ActionGetTables,
			Label:       "List tables",
			Description: "List visible tables, optionally within one schema.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "schema",
					Label:       "Schema",
					Type:        connectors.FieldString,
					Description: "Optional schema name.",
				},
				{
					Name:        "include_system",
					Label:       "Include system schemas",
					Type:        connectors.FieldBoolean,
					Description: "Include pg_catalog and information_schema tables.",
				},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: 1000},
		},
		{
			Name:        ActionDescribeTable,
			Label:       "Describe table",
			Description: "Describe columns and basic metadata for one table.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "schema",
					Label:       "Schema",
					Type:        connectors.FieldString,
					Description: "Optional schema name.",
				},
				{
					Name:        "table",
					Label:       "Table",
					Type:        connectors.FieldString,
					Required:    true,
					Description: "Table name.",
				},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: 500},
		},
		{
			Name:        ActionQueryReadonly,
			Label:       "Run read-only query",
			Description: "Run a bounded read-only SQL query.",
			Category:    "query",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "sql",
					Label:       "SQL",
					Type:        connectors.FieldMultiline,
					Required:    true,
					Description: "Read-only SQL. Writes and multi-statement SQL are rejected by the connector contract.",
				},
				{
					Name:        "max_rows",
					Label:       "Max rows",
					Type:        connectors.FieldNumber,
					Default:     defaultMaxRows,
					Description: "Maximum rows to return.",
				},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxRows, MaxBytes: 500000},
		},
	}, nil
}

func (Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	base := connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		ContextMaterial: map[string]any{
			"connector_kind": Kind,
			"target_ref":     req.Target.Ref,
			"profile_id":     req.Profile.ID,
			"action_name":    req.ActionName,
		},
	}

	switch req.ActionName {
	case ActionGetSchemas:
		base.Risk = connectors.RiskRead
		base.Title = "List Postgres schemas"
		base.Summary = targetSummary(req.Target, "List visible schemas")
		base.Preview = map[string]any{}
		base.Payload = map[string]any{}
		return base, nil
	case ActionGetTables:
		schema := cleanIdentifierInput(req.Input, "schema")
		includeSystem := boolInput(req.Input, "include_system")
		base.Risk = connectors.RiskRead
		base.Title = "List Postgres tables"
		base.Summary = targetSummary(req.Target, "List visible tables")
		base.Preview = map[string]any{"schema": schema, "include_system": includeSystem}
		base.Payload = map[string]any{"schema": schema, "include_system": includeSystem}
		base.ContextMaterial["schema"] = schema
		base.ContextMaterial["include_system"] = includeSystem
		return base, nil
	case ActionDescribeTable:
		schema := cleanIdentifierInput(req.Input, "schema")
		table := cleanIdentifierInput(req.Input, "table")
		if table == "" {
			return connectors.PreparedAction{}, fmt.Errorf("%s table is required", ActionDescribeTable)
		}
		base.Risk = connectors.RiskRead
		base.Title = "Describe Postgres table"
		base.Summary = targetSummary(req.Target, "Describe table metadata")
		base.Preview = map[string]any{"schema": schema, "table": table}
		base.Payload = map[string]any{"schema": schema, "table": table}
		base.ContextMaterial["schema"] = schema
		base.ContextMaterial["table"] = table
		return base, nil
	case ActionQueryReadonly:
		sql := strings.TrimSpace(stringInput(req.Input, "sql"))
		if err := validateReadonlySQL(sql); err != nil {
			return connectors.PreparedAction{}, err
		}
		limit := intInput(req.Input, "max_rows", defaultMaxRows)
		if limit < 1 {
			limit = defaultMaxRows
		}
		if limit > maxRows {
			limit = maxRows
		}
		base.Risk = connectors.RiskRead
		base.Title = "Run Postgres read-only query"
		base.Summary = targetSummary(req.Target, "Run a bounded read-only query")
		base.Preview = map[string]any{
			"sql":      sql,
			"max_rows": limit,
			"reason":   strings.TrimSpace(req.Reason),
		}
		base.Payload = map[string]any{
			"sql":      sql,
			"max_rows": limit,
			"reason":   strings.TrimSpace(req.Reason),
		}
		base.ContextMaterial["sql"] = sql
		base.ContextMaterial["max_rows"] = limit
		base.ContextMaterial["reason"] = strings.TrimSpace(req.Reason)
		return base, nil
	default:
		return connectors.PreparedAction{}, fmt.Errorf("%w: %s", ErrUnsupportedAction, req.ActionName)
	}
}

func (Connector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	return connectors.ActionResult{}, ErrExecutionNotWired
}

func targetSummary(target connectors.TargetView, action string) string {
	if target.Name == "" {
		return action + " on Postgres target."
	}
	return action + " on " + target.Name + "."
}

func validateReadonlySQL(sql string) error {
	if sql == "" {
		return fmt.Errorf("%s sql is required", ActionQueryReadonly)
	}
	if len(sql) > maxSQLBytes {
		return fmt.Errorf("%s sql exceeds %d bytes", ActionQueryReadonly, maxSQLBytes)
	}
	if strings.ContainsRune(sql, '\x00') {
		return fmt.Errorf("%s sql contains invalid null byte", ActionQueryReadonly)
	}
	normalized := strings.TrimSpace(stripTrailingStatementTerminator(stripLeadingSQLComments(sql)))
	if normalized == "" {
		return fmt.Errorf("%s sql is required", ActionQueryReadonly)
	}
	lower := strings.ToLower(normalized)
	if strings.Contains(lower, ";") {
		return fmt.Errorf("%s only accepts a single statement", ActionQueryReadonly)
	}
	if disallowedReadonlyTerms.MatchString(lower) {
		return fmt.Errorf("%s only accepts read-only SQL", ActionQueryReadonly)
	}
	if !hasReadonlyPrefix(lower) {
		return fmt.Errorf("%s only accepts SELECT, WITH, SHOW, or EXPLAIN SQL", ActionQueryReadonly)
	}
	return nil
}

func hasReadonlyPrefix(sql string) bool {
	for _, prefix := range []string{"select", "with", "show", "explain"} {
		if sql == prefix || strings.HasPrefix(sql, prefix+" ") || strings.HasPrefix(sql, prefix+"\n") || strings.HasPrefix(sql, prefix+"\t") {
			return true
		}
	}
	return false
}

func stripLeadingSQLComments(sql string) string {
	for {
		sql = strings.TrimSpace(sql)
		switch {
		case strings.HasPrefix(sql, "--"):
			lineEnd := strings.IndexByte(sql, '\n')
			if lineEnd < 0 {
				return ""
			}
			sql = sql[lineEnd+1:]
		case strings.HasPrefix(sql, "/*"):
			commentEnd := strings.Index(sql, "*/")
			if commentEnd < 0 {
				return sql
			}
			sql = sql[commentEnd+2:]
		default:
			return sql
		}
	}
}

func stripTrailingStatementTerminator(sql string) string {
	sql = strings.TrimSpace(sql)
	if strings.HasSuffix(sql, ";") {
		return strings.TrimSpace(strings.TrimSuffix(sql, ";"))
	}
	return sql
}

func cleanIdentifierInput(input map[string]any, name string) string {
	value := strings.TrimSpace(stringInput(input, name))
	value = strings.Trim(value, "\"")
	if strings.ContainsAny(value, ";\x00\n\r") {
		return ""
	}
	return value
}

func stringInput(input map[string]any, name string) string {
	if input == nil {
		return ""
	}
	value, ok := input[name]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func intInput(input map[string]any, name string, fallback int) int {
	if input == nil {
		return fallback
	}
	value, ok := input[name]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return fallback
	}
}

func boolInput(input map[string]any, name string) bool {
	if input == nil {
		return false
	}
	value, ok := input[name]
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
