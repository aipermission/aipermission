// Package clickhouseconnector implements the built-in ClickHouse connector.
package clickhouseconnector

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlsafe"
)

const (
	Kind    = "clickhouse"
	Label   = "ClickHouse"
	Version = "0.1"

	ActionGetDatabases  = "get_databases"
	ActionGetTables     = "get_tables"
	ActionDescribeTable = "describe_table"
	ActionQueryReadonly = "query_readonly"

	defaultMaxRows = 100
	maxRows        = 1000
	maxSQLBytes    = 20000
	maxOutputBytes = 500000
	maxCellBytes   = 64000
	defaultPort    = 9000
	queryTimeout   = 20 * time.Second
	slowQueryAfter = 2 * time.Second
)

const truncatedSuffix = "...[truncated]"

var (
	ErrUnsupportedAction = errors.New("unsupported clickhouse connector action")
	ErrMissingTransport  = errors.New("clickhouse connector network transport is unavailable")
	ErrMissingCredential = errors.New("clickhouse connector credential is missing required identity")
	ErrInvalidConfig     = errors.New("clickhouse connector target config is invalid")

	disallowedReadonlyTerms = regexp.MustCompile(`\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|attach|detach|optimize|kill|set|use|rename|exchange|move|call|into|outfile)\b`)
)

type Connector struct{}

func New() Connector { return Connector{} }

func (Connector) Kind() string    { return Kind }
func (Connector) Label() string   { return Label }
func (Connector) Version() string { return Version }

func (Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{
			Name:        "connection_mode",
			Label:       "Connection mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "direct",
			Description: "Connect directly from the local gateway or through an SSH connector profile.",
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
		{Name: "host", Label: "Host", Type: connectors.FieldString, Required: true, Description: "ClickHouse host as seen by the selected connection mode."},
		{Name: "port", Label: "Native port", Type: connectors.FieldNumber, Required: true, Default: defaultPort, Description: "ClickHouse native protocol port, commonly 9000 or 9440 with TLS."},
		{Name: "database", Label: "Default database", Type: connectors.FieldString, Required: true, Default: "default", Description: "Default ClickHouse database for this target."},
		{
			Name:        "tls_mode",
			Label:       "TLS mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "disable",
			Description: "Verify full validates the ClickHouse certificate and host name. Disable only for trusted local or SSH-tunneled endpoints.",
			Options: []connectors.FieldOption{
				{Value: "disable", Label: "Disable"},
				{Value: "verify_full", Label: "Verify full"},
			},
		},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{
		Kind:        "username_password",
		Label:       "Username and password",
		Description: "ClickHouse username and optional password stored through the encrypted vault layer.",
		Schema: connectors.Schema{Fields: []connectors.Field{
			{Name: "username", Label: "Username", Type: connectors.FieldString, Required: true, Description: "ClickHouse user for this profile."},
			{Name: "password", Label: "Password", Type: connectors.FieldSecret, Secret: true, Description: "ClickHouse password. It may be empty only when the selected user allows passwordless access."},
		}},
	}}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	title := "ClickHouse target"
	if target.Name != "" {
		title += ": " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Inspect ClickHouse metadata and run bounded read-only analytics SQL through AIPermission approval rules.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use get_databases and get_tables before composing SQL when the database shape is unknown.",
			"Use describe_table to inspect ordered columns before querying application data.",
			"Use query_readonly only for one SELECT, WITH, SHOW, or EXPLAIN statement and keep max_rows small.",
		},
		Warnings: []string{
			"Use a dedicated read-only ClickHouse user. Connector checks and server settings are defense in depth, not a replacement for database permissions.",
			"Queries are capped by timeout, row count, output bytes, and a read-only ClickHouse setting.",
			"Redaction is best-effort. Do not intentionally query secrets or sensitive datasets without explicit operator approval.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{
		{Name: ActionGetDatabases, Label: "List databases", Description: "List visible ClickHouse databases.", Category: "metadata", Risk: connectors.RiskRead, InputSchema: connectors.Schema{}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 500}},
		{
			Name: ActionGetTables, Label: "List tables", Description: "List visible ClickHouse tables, optionally within one database.", Category: "metadata", Risk: connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "database", Label: "Database", Type: connectors.FieldString, Description: "Optional database name. Empty lists tables across visible non-system databases."}}},
			OutputHint:  connectors.OutputHint{Format: "json", MaxRows: maxRows, MaxBytes: maxOutputBytes},
		},
		{
			Name: ActionDescribeTable, Label: "Describe table", Description: "Describe ordered columns and basic metadata for one ClickHouse table.", Category: "metadata", Risk: connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "database", Label: "Database", Type: connectors.FieldString, Description: "Optional database name. The target default is used when empty."},
				{Name: "table", Label: "Table", Type: connectors.FieldString, Required: true, Description: "Table name."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: 500, MaxBytes: maxOutputBytes},
		},
		{
			Name: ActionQueryReadonly, Label: "Run read-only query", Description: "Run one bounded read-only ClickHouse SQL query.", Category: "query", Risk: connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "sql", Label: "SQL", Type: connectors.FieldMultiline, Required: true, Description: "One SELECT, WITH, SHOW, or EXPLAIN statement. Writes and multi-statement SQL are rejected."},
				{Name: "max_rows", Label: "Max rows", Type: connectors.FieldNumber, Default: defaultMaxRows, Description: "Maximum rows to return."},
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
		Risk:          connectors.RiskRead,
		ContextMaterial: map[string]any{
			"connector_kind":       Kind,
			"target_ref":           req.Target.Ref,
			"profile_id":           req.Profile.ID,
			"action_name":          req.ActionName,
			"connection_mode":      connectionMode(req.Target),
			"transport_target_ref": targetString(req.Target.Config, "transport_target_ref"),
			"tls_mode":             tlsMode(req.Target.Config),
		},
	}

	switch req.ActionName {
	case ActionGetDatabases:
		base.Title = "List ClickHouse databases"
		base.Summary = targetSummary(req.Target, "List visible databases")
		base.Preview = map[string]any{}
		base.Payload = map[string]any{}
		return base, nil
	case ActionGetTables:
		database := cleanIdentifierInput(req.Input, "database")
		base.Title = "List ClickHouse tables"
		base.Summary = targetSummary(req.Target, "List visible tables")
		base.Preview = map[string]any{"database": database}
		base.Payload = map[string]any{"database": database}
		base.ContextMaterial["database"] = database
		return base, nil
	case ActionDescribeTable:
		database := cleanIdentifierInput(req.Input, "database")
		if database == "" {
			database = targetString(req.Target.Config, "database")
		}
		table := cleanIdentifierInput(req.Input, "table")
		if table == "" {
			return connectors.PreparedAction{}, fmt.Errorf("%s table is required", ActionDescribeTable)
		}
		base.Title = "Describe ClickHouse table"
		base.Summary = targetSummary(req.Target, "Describe table metadata")
		base.Preview = map[string]any{"database": database, "table": table}
		base.Payload = map[string]any{"database": database, "table": table}
		base.ContextMaterial["database"] = database
		base.ContextMaterial["table"] = table
		return base, nil
	case ActionQueryReadonly:
		sqlText := strings.TrimSpace(stringInput(req.Input, "sql"))
		if err := validateReadonlySQL(sqlText); err != nil {
			return connectors.PreparedAction{}, err
		}
		rowLimit := boundedRows(intInput(req.Input, "max_rows", defaultMaxRows))
		base.Title = "Run ClickHouse read-only query"
		base.Summary = targetSummary(req.Target, "Run a bounded read-only query")
		base.Preview = map[string]any{"sql": sqlText, "max_rows": rowLimit, "reason": strings.TrimSpace(req.Reason)}
		base.Payload = map[string]any{"sql": sqlText, "max_rows": rowLimit, "reason": strings.TrimSpace(req.Reason)}
		base.ContextMaterial["sql"] = sqlText
		base.ContextMaterial["max_rows"] = rowLimit
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
	db, err := connect(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer db.Close()

	started := time.Now()
	var output queryOutput
	switch action.ActionName {
	case ActionGetDatabases:
		output, err = queryRows(ctx, db, `
			SELECT name AS database
			FROM system.databases
			WHERE name NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
			ORDER BY name`, 500)
	case ActionGetTables:
		database := payloadString(action.Payload, "database")
		output, err = queryRows(ctx, db, `
			SELECT database, name AS table, engine, total_rows, total_bytes
			FROM system.tables
			WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
			  AND (? = '' OR database = ?)
			ORDER BY database, name`, maxRows, database, database)
	case ActionDescribeTable:
		database := payloadString(action.Payload, "database")
		table := payloadString(action.Payload, "table")
		if database == "" || table == "" {
			return connectors.ActionResult{}, fmt.Errorf("%s database and table are required", ActionDescribeTable)
		}
		output, err = queryRows(ctx, db, `
			SELECT database, table, position, name AS column_name, type AS data_type,
			       default_kind, default_expression, comment
			FROM system.columns
			WHERE database = ? AND table = ?
			ORDER BY position`, 500, database, table)
	case ActionQueryReadonly:
		sqlText := payloadString(action.Payload, "sql")
		if err := validateReadonlySQL(sqlText); err != nil {
			return connectors.ActionResult{}, err
		}
		output, err = queryRows(ctx, db, sqlText, boundedRows(payloadInt(action.Payload, "max_rows", defaultMaxRows)))
	default:
		return connectors.ActionResult{}, fmt.Errorf("%w: %s", ErrUnsupportedAction, action.ActionName)
	}
	if err != nil {
		return connectors.ActionResult{}, err
	}
	duration := time.Since(started)
	output.DurationMS = duration.Milliseconds()
	output.SlowQuery = duration >= slowQueryAfter
	output.Result = output.Result.Fit(maxOutputBytes, output.extraFields())
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output.ToMap(),
		DisplayText: output.DisplayText(),
		Metadata: map[string]any{
			"row_count":   output.RowCount,
			"truncated":   output.Truncated,
			"max_rows":    output.MaxRows,
			"duration_ms": output.DurationMS,
			"slow_query":  output.SlowQuery,
			"action":      action.ActionName,
			"target_ref":  action.TargetRef,
		},
	}, nil
}

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	db, err := connect(ctx, runtime)
	if err != nil {
		return connectors.TestResult{Status: classifyTestError(err), Message: err.Error()}, nil
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return connectors.TestResult{Status: classifyTestError(err), Message: fmt.Sprintf("ping clickhouse: %v", err)}, nil
	}
	return connectors.TestResult{Status: connectors.TestOK, Message: "ClickHouse connection succeeded"}, nil
}

func connect(ctx context.Context, runtime connectors.RuntimeContext) (*sql.DB, error) {
	username := strings.TrimSpace(publicString(runtime.Profile.Public, "username"))
	if username == "" {
		return nil, fmt.Errorf("%w: username", ErrMissingCredential)
	}
	password := ""
	if runtime.Secrets != nil {
		password, _ = runtime.Secrets.GetSecret(ctx, "password")
	}
	host := targetString(runtime.Target.Config, "host")
	database := targetString(runtime.Target.Config, "database")
	port := targetPort(runtime.Target.Config)
	if host == "" || database == "" || port < 1 {
		return nil, fmt.Errorf("%w: host, port, and database are required", ErrInvalidConfig)
	}
	transport, _ := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	dialRequest := connectors.NetworkDialRequest{Mode: connectionMode(runtime.Target), Host: host, Port: port, TransportTargetRef: targetString(runtime.Target.Config, "transport_target_ref")}
	tlsConfig := clickHouseTLSConfig(runtime.Target)
	options := &clickhouse.Options{
		Protocol: clickhouse.Native,
		Addr:     []string{net.JoinHostPort(host, strconv.Itoa(port))},
		Auth:     clickhouse.Auth{Database: database, Username: username, Password: password},
		Settings: clickhouse.Settings{
			"readonly":             1,
			"max_execution_time":   uint64(queryTimeout / time.Second),
			"max_result_rows":      uint64(maxRows + 1),
			"max_result_bytes":     uint64(maxOutputBytes),
			"result_overflow_mode": "break",
		},
		DialTimeout:     10 * time.Second,
		ReadTimeout:     queryTimeout,
		MaxOpenConns:    1,
		MaxIdleConns:    0,
		ConnMaxLifetime: time.Minute,
		TLS:             tlsConfig,
		DialContext: func(dialCtx context.Context, _ string) (net.Conn, error) {
			conn, err := transport.DialConnectorTCP(dialCtx, dialRequest)
			if err != nil {
				return nil, err
			}
			if tlsConfig == nil {
				return conn, nil
			}
			tlsConn := tls.Client(conn, tlsConfig.Clone())
			if err := tlsConn.HandshakeContext(dialCtx); err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("clickhouse TLS handshake: %w", err)
			}
			return tlsConn, nil
		},
	}
	return clickhouse.OpenDB(options), nil
}

func clickHouseTLSConfig(target connectors.TargetView) *tls.Config {
	if tlsMode(target.Config) != "verify_full" {
		return nil
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: targetString(target.Config, "host")}
}

type queryOutput struct {
	sqlresult.Result
	DurationMS int64
	SlowQuery  bool
}

func (o queryOutput) ToMap() map[string]any {
	return o.Result.ToMap(maxOutputBytes, o.extraFields())
}

func (o queryOutput) extraFields() map[string]any {
	return map[string]any{"duration_ms": o.DurationMS, "slow_query": o.SlowQuery}
}

func (o queryOutput) DisplayText() string {
	text := o.Result.DisplayText()
	if o.SlowQuery {
		text += fmt.Sprintf(" / slow query %dms", o.DurationMS)
	}
	return text
}

func queryRows(ctx context.Context, db *sql.DB, query string, rowLimit int, args ...any) (queryOutput, error) {
	rowLimit = boundedRows(rowLimit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return queryOutput{}, fmt.Errorf("query clickhouse: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return queryOutput{}, fmt.Errorf("read clickhouse columns: %w", err)
	}
	builder := sqlresult.NewBuilder(columns, rowLimit, maxOutputBytes, maxCellBytes, truncatedSuffix)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return queryOutput{}, fmt.Errorf("scan clickhouse row: %w", err)
		}
		if !builder.Add(values, normalizeValue) {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return queryOutput{}, fmt.Errorf("iterate clickhouse rows: %w", err)
	}
	return queryOutput{Result: builder.Result(nil)}, nil
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []byte:
		if utf8.Valid(typed) {
			return string(typed)
		}
		return "base64:" + base64.StdEncoding.EncodeToString(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case net.IP:
		return typed.String()
	}
	valueOf := reflect.ValueOf(value)
	if valueOf.IsValid() && (valueOf.Kind() == reflect.Pointer || valueOf.Kind() == reflect.Interface) {
		if valueOf.IsNil() {
			return nil
		}
		return normalizeValue(valueOf.Elem().Interface())
	}
	if _, err := json.Marshal(value); err == nil {
		return value
	}
	return fmt.Sprint(value)
}

func validateReadonlySQL(sqlText string) error {
	return sqlsafe.ValidateReadOnly(sqlText, ActionQueryReadonly, maxSQLBytes, []string{"select", "with", "show", "explain"}, "SELECT, WITH, SHOW, or EXPLAIN", disallowedReadonlyTerms)
}

func connectionMode(target connectors.TargetView) string {
	if strings.TrimSpace(targetString(target.Config, "connection_mode")) == "over_ssh" {
		return "over_ssh"
	}
	return "direct"
}

func tlsMode(config map[string]any) string {
	if strings.TrimSpace(targetString(config, "tls_mode")) == "verify_full" {
		return "verify_full"
	}
	return "disable"
}

func targetPort(config map[string]any) int {
	port := intInput(config, "port", defaultPort)
	if port < 1 || port > 65535 {
		return 0
	}
	return port
}

func boundedRows(value int) int {
	if value < 1 {
		return defaultMaxRows
	}
	if value > maxRows {
		return maxRows
	}
	return value
}

func cleanIdentifierInput(input map[string]any, name string) string {
	value := strings.TrimSpace(stringInput(input, name))
	value = strings.Trim(value, "\"`")
	if strings.ContainsAny(value, ";\x00\n\r") {
		return ""
	}
	return value
}

func stringInput(values map[string]any, name string) string {
	if values == nil {
		return ""
	}
	value, ok := values[name]
	if !ok || value == nil {
		return ""
	}
	if typed, ok := value.(string); ok {
		return typed
	}
	return fmt.Sprint(value)
}

func intInput(values map[string]any, name string, fallback int) int {
	if values == nil {
		return fallback
	}
	switch value := values[name].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := strconv.Atoi(value.String())
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func targetString(values map[string]any, name string) string {
	return strings.TrimSpace(stringInput(values, name))
}

func publicString(values map[string]any, name string) string {
	return strings.TrimSpace(stringInput(values, name))
}

func payloadString(values map[string]any, name string) string {
	return strings.TrimSpace(stringInput(values, name))
}

func payloadInt(values map[string]any, name string, fallback int) int {
	return intInput(values, name, fallback)
}

func targetSummary(target connectors.TargetView, action string) string {
	if target.Name == "" {
		return action + " on ClickHouse target."
	}
	return action + " on " + target.Name + "."
}

func classifyTestError(err error) connectors.TestStatus {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "authentication"), strings.Contains(message, "password"), strings.Contains(message, "code: 516"):
		return connectors.TestFailedAuth
	case strings.Contains(message, "not enough privileges"), strings.Contains(message, "permission"), strings.Contains(message, "readonly"):
		return connectors.TestFailedPermission
	case strings.Contains(message, "tls"), strings.Contains(message, "certificate"):
		return connectors.TestFailedTLS
	case strings.Contains(message, "connect"), strings.Contains(message, "timeout"), strings.Contains(message, "refused"), strings.Contains(message, "no such host"):
		return connectors.TestFailedNetwork
	default:
		return connectors.TestUnknownError
	}
}
