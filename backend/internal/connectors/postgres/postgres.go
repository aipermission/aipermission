// Package postgresconnector defines the Postgres connector contract.
package postgresconnector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5"
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
	maxOutputBytes = 500000
	maxCellBytes   = 64000
	queryTimeout   = 20 * time.Second
)

const truncatedSuffix = "...[truncated]"

var (
	ErrUnsupportedAction = errors.New("unsupported postgres connector action")
	ErrUnsupportedMode   = errors.New("postgres connector connection mode is not supported yet")
	ErrMissingSecret     = errors.New("postgres connector credential is missing required secret")
	ErrInvalidConfig     = errors.New("postgres connector target config is invalid")

	disallowedReadonlyTerms = regexp.MustCompile(`\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|copy|call|do|vacuum|analyze|reindex|cluster|refresh|merge)\b`)
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
					Type:        connectors.FieldNumber,
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

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if runtime.Target.ConnectorKind != Kind {
		return connectors.ActionResult{}, fmt.Errorf("target connector kind must be %s", Kind)
	}
	if connectionMode(runtime.Target) != "direct" {
		return connectors.ActionResult{}, ErrUnsupportedMode
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
	if connectionMode(runtime.Target) != "direct" {
		return connectors.TestResult{Status: connectors.TestUnknownError, Message: "unsupported postgres connection mode"}, ErrUnsupportedMode
	}
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
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"row_count"`
	MaxRows   int              `json:"max_rows"`
	Truncated bool             `json:"truncated"`
}

func (o queryOutput) ToMap() map[string]any {
	return map[string]any{
		"columns":   o.Columns,
		"rows":      o.Rows,
		"row_count": o.RowCount,
		"max_rows":  o.MaxRows,
		"max_bytes": maxOutputBytes,
		"truncated": o.Truncated,
	}
}

func (o queryOutput) DisplayText() string {
	text := fmt.Sprintf("%d row", o.RowCount)
	if o.RowCount != 1 {
		text += "s"
	}
	if o.Truncated {
		text += " (truncated)"
	}
	return text
}

func connect(ctx context.Context, runtime connectors.RuntimeContext) (*pgx.Conn, error) {
	username := strings.TrimSpace(publicString(runtime.Profile.Public, "username"))
	if username == "" && runtime.Secrets != nil {
		secretUsername, err := runtime.Secrets.GetSecret(ctx, "username")
		if err == nil {
			username = strings.TrimSpace(secretUsername)
		}
	}
	if username == "" {
		return nil, fmt.Errorf("%w: username", ErrMissingSecret)
	}
	if runtime.Secrets == nil {
		return nil, fmt.Errorf("%w: password", ErrMissingSecret)
	}
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if err != nil || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("%w: password", ErrMissingSecret)
	}

	host := targetString(runtime.Target.Config, "host")
	database := targetString(runtime.Target.Config, "database")
	if host == "" {
		return nil, fmt.Errorf("%w: host is required", ErrInvalidConfig)
	}
	if database == "" {
		return nil, fmt.Errorf("%w: database is required", ErrInvalidConfig)
	}

	connURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(targetPort(runtime.Target.Config))),
		Path:   "/" + database,
	}
	query := connURL.Query()
	query.Set("sslmode", sslMode(runtime.Target.Config))
	query.Set("connect_timeout", "10")
	connURL.RawQuery = query.Encode()

	config, err := pgx.ParseConfig(connURL.String())
	if err != nil {
		return nil, fmt.Errorf("parse postgres connection config: %w", err)
	}
	if config.RuntimeParams == nil {
		config.RuntimeParams = map[string]string{}
	}
	config.RuntimeParams["application_name"] = "aipermission"
	config.RuntimeParams["statement_timeout"] = strconv.Itoa(int(queryTimeout.Milliseconds()))
	config.RuntimeParams["default_transaction_read_only"] = "on"

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	return conn, nil
}

func getTables(ctx context.Context, tx pgx.Tx, schema string, includeSystem bool) (queryOutput, error) {
	query := `
		SELECT table_schema, table_name, table_type
		FROM information_schema.tables
		WHERE ($1 = '' OR table_schema = $1)`
	if !includeSystem {
		query += ` AND table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema NOT LIKE 'pg_toast%'`
	}
	query += ` ORDER BY table_schema, table_name`
	return queryRows(ctx, tx, query, 1000, schema)
}

func describeTable(ctx context.Context, tx pgx.Tx, schema string, table string) (queryOutput, error) {
	query := `
		SELECT table_schema, table_name, ordinal_position, column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_name = $1
			AND ($2 = '' OR table_schema = $2)
			AND table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name, ordinal_position`
	return queryRows(ctx, tx, query, 500, table, schema)
}

func queryRows(ctx context.Context, tx pgx.Tx, sql string, rowLimit int, args ...any) (queryOutput, error) {
	if rowLimit < 1 {
		rowLimit = defaultMaxRows
	}
	if rowLimit > maxRows {
		rowLimit = maxRows
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return queryOutput{}, fmt.Errorf("query postgres: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		columns = append(columns, field.Name)
	}
	items := []map[string]any{}
	outputBytes := 0
	truncated := false
	for rows.Next() {
		if outputBytes >= maxOutputBytes {
			return queryOutput{Columns: columns, Rows: items, RowCount: len(items), MaxRows: rowLimit, Truncated: true}, nil
		}
		values, err := rows.Values()
		if err != nil {
			return queryOutput{}, fmt.Errorf("read postgres row: %w", err)
		}
		if len(items) >= rowLimit {
			return queryOutput{
				Columns:   columns,
				Rows:      items,
				RowCount:  len(items),
				MaxRows:   rowLimit,
				Truncated: true,
			}, nil
		}
		item := make(map[string]any, len(columns))
		for index, column := range columns {
			var value any
			if index < len(values) {
				var valueBytes int
				var valueTruncated bool
				value, valueBytes, valueTruncated = boundPostgresValue(normalizePostgresValue(values[index]), maxOutputBytes-outputBytes)
				outputBytes += valueBytes
				if valueTruncated {
					truncated = true
				}
			}
			item[column] = value
			if outputBytes >= maxOutputBytes {
				truncated = true
				break
			}
		}
		items = append(items, item)
		if truncated && outputBytes >= maxOutputBytes {
			return queryOutput{Columns: columns, Rows: items, RowCount: len(items), MaxRows: rowLimit, Truncated: true}, nil
		}
	}
	if err := rows.Err(); err != nil {
		return queryOutput{}, fmt.Errorf("iterate postgres rows: %w", err)
	}
	return queryOutput{Columns: columns, Rows: items, RowCount: len(items), MaxRows: rowLimit, Truncated: truncated}, nil
}

func normalizePostgresValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return typed
	}
}

func boundPostgresValue(value any, remainingBytes int) (any, int, bool) {
	if remainingBytes <= 0 {
		return truncateStringWithSuffix("", 0), 0, true
	}
	limit := minInt(maxCellBytes, remainingBytes)
	switch typed := value.(type) {
	case string:
		if len(typed) > limit {
			truncated := truncateStringWithSuffix(typed, limit)
			return truncated, len(truncated), true
		}
		return typed, len(typed), false
	case nil:
		return nil, minInt(4, remainingBytes), false
	default:
		encoded, err := json.Marshal(typed)
		if err == nil && len(encoded) <= limit {
			return typed, len(encoded), false
		}
		text := fmt.Sprint(typed)
		truncated := truncateStringWithSuffix(text, limit)
		return truncated, len(truncated), true
	}
}

func truncateStringWithSuffix(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	if limit <= len(truncatedSuffix) {
		return truncateUTF8Bytes(truncatedSuffix, limit)
	}
	return truncateUTF8Bytes(value, limit-len(truncatedSuffix)) + truncatedSuffix
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	cut := 0
	for index := range value {
		if index > limit {
			break
		}
		cut = index
	}
	if cut == 0 {
		return ""
	}
	return value[:cut]
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
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

func sslMode(config map[string]any) string {
	mode := targetString(config, "ssl_mode")
	switch mode {
	case "disable", "prefer", "require", "verify-full", "verify_full":
		if mode == "verify_full" {
			return "verify-full"
		}
		return mode
	default:
		return "prefer"
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
