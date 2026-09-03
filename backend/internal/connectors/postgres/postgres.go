// Package postgresconnector defines the Postgres connector contract.
package postgresconnector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlsafe"
	"github.com/jackc/pgx/v5"
)

const (
	Kind    = "postgres"
	Label   = "Postgres"
	Version = "0.2"

	ActionGetSchemas    = "get_schemas"
	ActionGetTables     = "get_tables"
	ActionDescribeTable = "describe_table"
	ActionQueryReadonly = "query_readonly"

	defaultMaxRows = 100
	maxRows        = 1000
	maxSQLBytes    = 20000
	maxOutputBytes = 500000
	maxCellBytes   = 64000
	queryTimeout   = 20 * time.Second
	backupTimeout  = 2 * time.Minute
	restoreTimeout = 5 * time.Minute
	maxBackupBytes = 256 << 20
	maxRestoreLog  = 2 << 20
)

const truncatedSuffix = "...[truncated]"

var (
	ErrUnsupportedAction = errors.New("unsupported postgres connector action")
	ErrMissingTransport  = errors.New("postgres connector network transport is unavailable")
	ErrMissingSecret     = errors.New("postgres connector credential is missing required secret")
	ErrInvalidConfig     = errors.New("postgres connector target config is invalid")

	disallowedReadonlyTerms = regexp.MustCompile(`\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|copy|call|do|vacuum|analyze|reindex|cluster|refresh|merge|into|notify|listen|unlisten|set|reset|lock|execute|prepare|deallocate|discard|comment|checkpoint|begin|start|commit|rollback|savepoint|release)\b`)
)

// Connector describes Postgres as a connector-shaped target with bounded
// metadata and read-only query actions.
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
			Description: "Direct connection from the local gateway to a reachable Postgres host.",
			Options: []connectors.FieldOption{
				{Value: "direct", Label: "Direct"},
				{Value: "over_ssh", Label: "Over SSH"},
			},
		},
		{
			Name:        "transport_target_ref",
			Label:       "SSH transport profile",
			Type:        connectors.FieldString,
			Description: "Connector target profile ref used when connection_mode is over_ssh.",
		},
		{
			Name:        "host",
			Label:       "Host",
			Type:        connectors.FieldString,
			Required:    true,
			Description: "Postgres host or service address as seen by the selected connection mode.",
		},
		{
			Name:        "port",
			Label:       "Port",
			Type:        connectors.FieldInteger,
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
			Default:     "auto",
			Description: "Auto verifies the server certificate and hostname for direct remote connections, and requires encryption for localhost or SSH-tunneled connections. Require encrypts traffic without verifying server identity.",
			Options: []connectors.FieldOption{
				{Value: "auto", Label: "Auto (recommended)"},
				{Value: "verify_full", Label: "Verify full"},
				{Value: "require", Label: "Require (no identity verification)"},
				{Value: "prefer", Label: "Prefer"},
				{Value: "disable", Label: "Disable"},
			},
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
				{
					Name:        "managed_by_aipermission",
					Label:       "Managed by AIPermission",
					Type:        connectors.FieldBoolean,
					Description: "True when AIPermission provisioned this database role and should clean it up when the profile is deleted.",
				},
				{
					Name:        "managed_role_name",
					Label:       "Managed role name",
					Type:        connectors.FieldString,
					Description: "Database role name created by AIPermission.",
				},
				{
					Name:        "managed_admin_profile_id",
					Label:       "Managed admin profile ID",
					Type:        connectors.FieldInteger,
					Description: "Credential profile used to create and clean up the managed role.",
				},
				{
					Name:        "managed_admin_profile_ref",
					Label:       "Managed admin profile label",
					Type:        connectors.FieldString,
					Description: "Credential profile label used to create the managed role.",
				},
				{
					Name:        "managed_preset",
					Label:       "Managed preset",
					Type:        connectors.FieldString,
					Description: "Provisioning preset used for this managed role.",
				},
				{
					Name:        "managed_scope",
					Label:       "Managed scope",
					Type:        connectors.FieldJSON,
					Description: "Provisioning scope summary for this managed role.",
				},
			}},
		},
	}
}

func (Connector) NormalizeTargetConfigUpdate(existing, submitted map[string]any) map[string]any {
	normalized := make(map[string]any, len(submitted)+1)
	for name, value := range submitted {
		normalized[name] = value
	}
	if submittedMode := strings.TrimSpace(targetString(normalized, "ssl_mode")); submittedMode != "" {
		return normalized
	}
	delete(normalized, "ssl_mode")
	if existingMode := strings.TrimSpace(targetString(existing, "ssl_mode")); validPostgresSSLMode(existingMode) {
		if existingMode == "verify-full" {
			existingMode = "verify_full"
		}
		normalized["ssl_mode"] = existingMode
	} else {
		normalized["ssl_mode"] = "require"
	}
	return normalized
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

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
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
					Type:        connectors.FieldInteger,
					Default:     defaultMaxRows,
					Description: "Maximum rows to return.",
				},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxRows, MaxBytes: maxOutputBytes},
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
			"connector_kind":       Kind,
			"target_ref":           req.Target.Ref,
			"profile_id":           req.Profile.ID,
			"action_name":          req.ActionName,
			"connection_mode":      connectionMode(req.Target),
			"transport_target_ref": targetString(req.Target.Config, "transport_target_ref"),
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

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if runtime.Target.ConnectorKind != Kind {
		return connectors.ActionResult{}, fmt.Errorf("target connector kind must be %s", Kind)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	conn, err := connect(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer conn.Close(context.Background())

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("start read-only transaction: %w", err)
	}
	defer tx.Rollback(context.Background())

	var output queryOutput
	switch action.ActionName {
	case ActionGetSchemas:
		output, err = queryRows(ctx, tx, `
			SELECT nspname AS schema
			FROM pg_namespace
			WHERE nspname NOT LIKE 'pg_%' AND nspname <> 'information_schema'
			ORDER BY nspname`,
			200,
		)
	case ActionGetTables:
		schema := payloadString(action.Payload, "schema")
		includeSystem := payloadBool(action.Payload, "include_system")
		output, err = getTables(ctx, tx, schema, includeSystem)
	case ActionDescribeTable:
		schema := payloadString(action.Payload, "schema")
		table := payloadString(action.Payload, "table")
		if table == "" {
			return connectors.ActionResult{}, fmt.Errorf("%s table is required", ActionDescribeTable)
		}
		output, err = describeTable(ctx, tx, schema, table)
	case ActionQueryReadonly:
		sql := payloadString(action.Payload, "sql")
		if err := validateReadonlySQL(sql); err != nil {
			return connectors.ActionResult{}, err
		}
		output, err = queryRows(ctx, tx, sql, payloadInt(action.Payload, "max_rows", defaultMaxRows))
	default:
		return connectors.ActionResult{}, fmt.Errorf("%w: %s", ErrUnsupportedAction, action.ActionName)
	}
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return connectors.ActionResult{}, fmt.Errorf("commit read-only transaction: %w", err)
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output.ToMap(),
		DisplayText: output.DisplayText(),
		Metadata: map[string]any{
			"row_count":  output.RowCount,
			"truncated":  output.Truncated,
			"max_rows":   output.MaxRows,
			"action":     action.ActionName,
			"target_ref": action.TargetRef,
		},
	}, nil
}

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	conn, err := connect(ctx, runtime)
	if err != nil {
		return connectors.TestResult{Status: classifyTestError(err), Message: err.Error()}, nil
	}
	defer conn.Close(context.Background())
	var one int
	if err := conn.QueryRow(ctx, "select 1").Scan(&one); err != nil {
		return connectors.TestResult{Status: classifyTestError(err), Message: err.Error()}, nil
	}
	return connectors.TestResult{Status: connectors.TestOK, Message: "Postgres connection succeeded"}, nil
}

type queryOutput struct {
	sqlresult.Result
}

func (o queryOutput) ToMap() map[string]any {
	return o.Result.ToMap(maxOutputBytes, nil)
}

func (o queryOutput) DisplayText() string {
	return o.Result.DisplayText()
}

func connectionMode(target connectors.TargetView) string {
	mode := strings.TrimSpace(targetString(target.Config, "connection_mode"))
	if mode == "" {
		return "direct"
	}
	return mode
}

func targetString(config map[string]any, name string) string {
	if config == nil {
		return ""
	}
	value, ok := config[name]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func targetPort(config map[string]any) int {
	value := intInput(config, "port", 5432)
	if value < 1 || value > 65535 {
		return 5432
	}
	return value
}

func sslMode(target connectors.TargetView) string {
	return postgresTLSPlanForTarget(target).Mode
}

type postgresTLSPlan struct {
	Mode           string
	UseSystemRoots bool
}

func postgresTLSPlanForTarget(target connectors.TargetView) postgresTLSPlan {
	mode := targetString(target.Config, "ssl_mode")
	switch mode {
	case "disable", "prefer", "require", "verify-full", "verify_full":
		if mode == "verify_full" {
			mode = "verify-full"
		}
	case "":
		// Targets created before ssl_mode was persisted used require at runtime.
		mode = "require"
	case "auto":
		if connectors.UseVerifiedTLSByDefault(connectionMode(target), targetString(target.Config, "host")) {
			mode = "verify-full"
		} else {
			mode = "require"
		}
	default:
		mode = "require"
	}
	return postgresTLSPlan{Mode: mode, UseSystemRoots: mode == "verify-full"}
}

func validPostgresSSLMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "auto", "disable", "prefer", "require", "verify-full", "verify_full":
		return true
	default:
		return false
	}
}

func publicString(public map[string]any, name string) string {
	if public == nil {
		return ""
	}
	value, ok := public[name]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func int64Public(public map[string]any, name string) int64 {
	if public == nil {
		return 0
	}
	value, ok := public[name]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func clonePublicMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func payloadString(payload map[string]any, name string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[name]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func payloadBool(payload map[string]any, name string) bool {
	return boolInput(payload, name)
}

func payloadInt(payload map[string]any, name string, fallback int) int {
	value := intInput(payload, name, fallback)
	if value < 1 {
		return fallback
	}
	if value > maxRows {
		return maxRows
	}
	return value
}

func classifyTestError(err error) connectors.TestStatus {
	if errors.Is(err, connectors.ErrSecretProvider) {
		return connectors.TestUnknownError
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "password authentication failed"),
		strings.Contains(message, "authentication failed"):
		return connectors.TestFailedAuth
	case strings.Contains(message, "permission denied"):
		return connectors.TestFailedPermission
	case strings.Contains(message, "tls"), strings.Contains(message, "ssl"):
		return connectors.TestFailedTLS
	case strings.Contains(message, "connect"), strings.Contains(message, "timeout"), strings.Contains(message, "refused"), strings.Contains(message, "no such host"):
		return connectors.TestFailedNetwork
	default:
		return connectors.TestUnknownError
	}
}

func targetSummary(target connectors.TargetView, action string) string {
	if target.Name == "" {
		return action + " on Postgres target."
	}
	return action + " on " + target.Name + "."
}

func validateReadonlySQL(sql string) error {
	return sqlsafe.ValidateReadOnly(
		sql,
		ActionQueryReadonly,
		maxSQLBytes,
		[]string{"select", "with", "show", "explain"},
		"SELECT, WITH, SHOW, or EXPLAIN",
		disallowedReadonlyTerms,
	)
}

func cleanIdentifierInput(input map[string]any, name string) string {
	value := strings.TrimSpace(stringInput(input, name))
	value = strings.Trim(value, "\"")
	if strings.ContainsAny(value, ";\x00\n\r") {
		return ""
	}
	return value
}

func cleanSimpleIdentifierInput(input map[string]any, name string) string {
	return cleanSimpleIdentifierValue(stringInput(input, name))
}

func cleanSimpleIdentifierValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"")
	if value == "" || len(value) > 63 {
		return ""
	}
	for index, ch := range value {
		valid := ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (index > 0 && ch >= '0' && ch <= '9')
		if !valid {
			return ""
		}
	}
	return value
}

func qualifiedIdentifierSQL(schema string, table string) string {
	if schema == "" {
		return quoteIdentifier(table)
	}
	return quoteIdentifier(schema) + "." + quoteIdentifier(table)
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
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

func anySlice(value any) []any {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func stringSlice(value any) []string {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return out
	default:
		return nil
	}
}
