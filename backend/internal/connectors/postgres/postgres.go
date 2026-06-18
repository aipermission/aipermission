// Package postgresconnector defines the Postgres connector contract.
package postgresconnector

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
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
			Default:     "require",
			Description: "Postgres SSL mode. Use prefer or disable only when you intentionally accept weaker transport protection or connect through a trusted SSH tunnel.",
			Options: []connectors.FieldOption{
				{Value: "require", Label: "Require"},
				{Value: "verify_full", Label: "Verify full"},
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
					Type:        connectors.FieldNumber,
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

func (Connector) ProvisionCredentialProfile(ctx context.Context, runtime connectors.RuntimeContext, input map[string]any) (connectors.ProvisionedCredentialProfile, error) {
	if runtime.Target.ConnectorKind != Kind {
		return connectors.ProvisionedCredentialProfile{}, fmt.Errorf("target connector kind must be %s", Kind)
	}
	roleName := cleanSimpleIdentifierInput(input, "role_name")
	if roleName == "" {
		return connectors.ProvisionedCredentialProfile{}, fmt.Errorf("role_name is required and must be a simple identifier")
	}
	profileLabel := strings.TrimSpace(stringInput(input, "profile_label"))
	if profileLabel == "" {
		profileLabel = roleName
	}
	preset := strings.TrimSpace(stringInput(input, "preset"))
	switch preset {
	case "", "read_only":
		preset = "read_only"
	case "read_write":
	default:
		return connectors.ProvisionedCredentialProfile{}, fmt.Errorf("unsupported preset %q", preset)
	}
	scope, err := provisionScopeInput(input)
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	password, err := randomCredentialPassword()
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	statements, summary, err := provisionRoleStatements(runtime.Target, roleName, password, preset, scope)
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	conn, err := connectPostgres(ctx, runtime, false)
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, fmt.Errorf("start postgres role provisioning transaction: %w", err)
	}
	defer tx.Rollback(context.Background())
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return connectors.ProvisionedCredentialProfile{}, fmt.Errorf("provision postgres role: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return connectors.ProvisionedCredentialProfile{}, fmt.Errorf("commit postgres role provisioning: %w", err)
	}

	public := map[string]any{
		"username":                  roleName,
		"managed_by_aipermission":   true,
		"managed_role_name":         roleName,
		"managed_admin_profile_id":  runtime.Profile.ID,
		"managed_admin_profile_ref": runtime.Profile.Label,
		"managed_preset":            preset,
		"managed_scope":             summary,
	}
	return connectors.ProvisionedCredentialProfile{
		Kind:      "username_password",
		Label:     profileLabel,
		Public:    public,
		Secret:    map[string]any{"password": password},
		RiskLabel: provisionRiskLabel(preset),
		Result: connectors.ActionResult{
			Status:      connectors.ResultCompleted,
			Output:      map[string]any{"role_name": roleName, "profile_label": profileLabel, "preset": preset, "scope": summary},
			DisplayText: "Created Postgres role and saved credential profile",
		},
	}, nil
}

func (Connector) CleanupProvisionedCredentialProfile(ctx context.Context, runtime connectors.RuntimeContext, profile connectors.CredentialProfileView) (connectors.ActionResult, error) {
	if runtime.Target.ConnectorKind != Kind {
		return connectors.ActionResult{}, fmt.Errorf("target connector kind must be %s", Kind)
	}
	if !boolPublic(profile.Public, "managed_by_aipermission") {
		return connectors.ActionResult{Status: connectors.ResultCompleted, DisplayText: "No external cleanup required"}, nil
	}
	roleName := cleanSimpleIdentifierValue(publicString(profile.Public, "managed_role_name"))
	if roleName == "" {
		roleName = cleanSimpleIdentifierValue(publicString(profile.Public, "username"))
	}
	if roleName == "" {
		return connectors.ActionResult{}, fmt.Errorf("managed Postgres profile is missing a role name")
	}
	adminRole := cleanSimpleIdentifierValue(publicString(runtime.Profile.Public, "username"))
	if adminRole == "" {
		return connectors.ActionResult{}, fmt.Errorf("admin profile is missing a username for managed role cleanup")
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	conn, err := connectPostgres(ctx, runtime, false)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer conn.Close(context.Background())
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, roleName).Scan(&exists); err != nil {
		return connectors.ActionResult{}, fmt.Errorf("check postgres role: %w", err)
	}
	if !exists {
		return connectors.ActionResult{
			Status:      connectors.ResultCompleted,
			Output:      map[string]any{"role_name": roleName, "dropped": false, "reason": "role not found"},
			DisplayText: "Managed Postgres role was already absent",
		}, nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("start postgres role cleanup transaction: %w", err)
	}
	defer tx.Rollback(context.Background())
	for _, statement := range []string{
		fmt.Sprintf("REASSIGN OWNED BY %s TO %s", quoteIdentifier(roleName), quoteIdentifier(adminRole)),
		fmt.Sprintf("DROP OWNED BY %s", quoteIdentifier(roleName)),
		fmt.Sprintf("DROP ROLE %s", quoteIdentifier(roleName)),
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return connectors.ActionResult{}, fmt.Errorf("cleanup postgres role: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return connectors.ActionResult{}, fmt.Errorf("commit postgres role cleanup: %w", err)
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"role_name": roleName, "dropped": true},
		DisplayText: "Dropped managed Postgres role",
	}, nil
}

func (Connector) Backup(ctx context.Context, runtime connectors.RuntimeContext, _ connectors.BackupRequest) (connectors.BackupArtifact, error) {
	ctx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()
	invocation, err := postgresCLIConnection(ctx, runtime)
	if err != nil {
		return connectors.BackupArtifact{}, err
	}
	defer invocation.Cleanup()
	args := invocation.Args
	args = append(args,
		"--format=plain",
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
	)
	var stdout limitedBuffer
	stdout.Limit = maxBackupBytes
	var stderr limitedBuffer
	stderr.Limit = maxRestoreLog
	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	cmd.Env = invocation.Env
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return connectors.BackupArtifact{}, postgresCommandError("pg_dump", err, stderr.String())
	}
	database := targetString(runtime.Target.Config, "database")
	filename := postgresSafeFilename(runtime.Target.Name, database) + ".sql"
	return connectors.BackupArtifact{
		Filename:    filename,
		ContentType: "application/sql; charset=utf-8",
		Data:        stdout.Bytes(),
		Metadata: map[string]any{
			"connector_kind": Kind,
			"database":       database,
			"format":         "plain_sql",
			"clean":          true,
		},
	}, nil
}

func (Connector) Restore(ctx context.Context, runtime connectors.RuntimeContext, request connectors.RestoreRequest) (connectors.ActionResult, error) {
	if len(request.Data) == 0 {
		return connectors.ActionResult{}, fmt.Errorf("restore SQL file is empty")
	}
	if len(request.Data) > maxBackupBytes {
		return connectors.ActionResult{}, fmt.Errorf("restore SQL file is too large; maximum restore size is 256 MiB")
	}
	ctx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()
	invocation, err := postgresCLIConnection(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer invocation.Cleanup()
	args := invocation.Args
	args = append(args,
		"--single-transaction",
		"--set", "ON_ERROR_STOP=on",
	)
	var stdout limitedBuffer
	stdout.Limit = maxRestoreLog
	var stderr limitedBuffer
	stderr.Limit = maxRestoreLog
	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = invocation.Env
	cmd.Stdin = bytes.NewReader(request.Data)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return connectors.ActionResult{}, postgresCommandError("psql", err, stderr.String())
	}
	output := map[string]any{
		"filename": strings.TrimSpace(request.Filename),
		"stdout":   stdout.String(),
		"stderr":   stderr.String(),
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: "Postgres SQL restore completed",
		Metadata: map[string]any{
			"connector_kind": Kind,
			"database":       targetString(runtime.Target.Config, "database"),
			"filename":       strings.TrimSpace(request.Filename),
		},
	}, nil
}

func connect(ctx context.Context, runtime connectors.RuntimeContext) (*pgx.Conn, error) {
	return connectPostgres(ctx, runtime, true)
}

type postgresCLIInvocation struct {
	Env     []string
	Args    []string
	Cleanup func()
}

func postgresCLIConnection(ctx context.Context, runtime connectors.RuntimeContext) (postgresCLIInvocation, error) {
	username := strings.TrimSpace(publicString(runtime.Profile.Public, "username"))
	if username == "" {
		return postgresCLIInvocation{}, fmt.Errorf("%w: username", ErrMissingSecret)
	}
	if runtime.Secrets == nil {
		return postgresCLIInvocation{}, fmt.Errorf("%w: password", ErrMissingSecret)
	}
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if err != nil || strings.TrimSpace(password) == "" {
		return postgresCLIInvocation{}, fmt.Errorf("%w: password", ErrMissingSecret)
	}
	host := targetString(runtime.Target.Config, "host")
	database := targetString(runtime.Target.Config, "database")
	if host == "" {
		return postgresCLIInvocation{}, fmt.Errorf("%w: host is required", ErrInvalidConfig)
	}
	if database == "" {
		return postgresCLIInvocation{}, fmt.Errorf("%w: database is required", ErrInvalidConfig)
	}
	port := targetPort(runtime.Target.Config)
	cleanup := func() {}
	if connectionMode(runtime.Target) == "over_ssh" {
		localHost, localPort, stop, err := startPostgresTunnel(ctx, runtime)
		if err != nil {
			return postgresCLIInvocation{}, err
		}
		host = localHost
		port = localPort
		cleanup = stop
	}
	env := append(os.Environ(),
		"PGPASSWORD="+password,
		"PGSSLMODE="+sslMode(runtime.Target.Config),
		"PGCONNECT_TIMEOUT=10",
		"PGAPPNAME=aipermission",
	)
	args := []string{
		"--host", host,
		"--port", strconv.Itoa(port),
		"--username", username,
		"--dbname", database,
		"--no-password",
	}
	return postgresCLIInvocation{Env: env, Args: args, Cleanup: cleanup}, nil
}

func connectPostgres(ctx context.Context, runtime connectors.RuntimeContext, readOnly bool) (*pgx.Conn, error) {
	username := strings.TrimSpace(publicString(runtime.Profile.Public, "username"))
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
	transport, err := postgresNetworkTransport(runtime)
	if err != nil {
		return nil, err
	}
	dialRequest := postgresNetworkDialRequest(runtime.Target)
	config.Config.DialFunc = func(ctx context.Context, network string, address string) (net.Conn, error) {
		return transport.DialConnectorTCP(ctx, dialRequest)
	}
	if config.RuntimeParams == nil {
		config.RuntimeParams = map[string]string{}
	}
	config.RuntimeParams["application_name"] = "aipermission"
	config.RuntimeParams["statement_timeout"] = strconv.Itoa(int(queryTimeout.Milliseconds()))
	if readOnly {
		config.RuntimeParams["default_transaction_read_only"] = "on"
	}

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	return conn, nil
}

func postgresNetworkTransport(runtime connectors.RuntimeContext) (connectors.NetworkTransport, error) {
	transport, _ := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	return transport, nil
}

func postgresNetworkDialRequest(target connectors.TargetView) connectors.NetworkDialRequest {
	return connectors.NetworkDialRequest{
		Mode:               connectionMode(target),
		Host:               targetString(target.Config, "host"),
		Port:               targetPort(target.Config),
		TransportTargetRef: strings.TrimSpace(targetString(target.Config, "transport_target_ref")),
	}
}

func startPostgresTunnel(ctx context.Context, runtime connectors.RuntimeContext) (string, int, func(), error) {
	transport, err := postgresNetworkTransport(runtime)
	if err != nil {
		return "", 0, nil, err
	}
	request := postgresNetworkDialRequest(runtime.Target)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", 0, nil, fmt.Errorf("start postgres local tunnel: %w", err)
	}
	tunnelCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			localConn, err := listener.Accept()
			if err != nil {
				return
			}
			go pipePostgresTunnelConn(tunnelCtx, transport, request, localConn)
		}
	}()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		cancel()
		_ = listener.Close()
		<-done
		return "", 0, nil, fmt.Errorf("postgres local tunnel address is not TCP")
	}
	cleanup := func() {
		cancel()
		_ = listener.Close()
		<-done
	}
	return "127.0.0.1", addr.Port, cleanup, nil
}

func pipePostgresTunnelConn(ctx context.Context, transport connectors.NetworkTransport, request connectors.NetworkDialRequest, localConn net.Conn) {
	remoteConn, err := transport.DialConnectorTCP(ctx, request)
	if err != nil {
		_ = localConn.Close()
		return
	}
	copyDone := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remoteConn, localConn)
		_ = remoteConn.Close()
		_ = localConn.Close()
		copyDone <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(localConn, remoteConn)
		_ = localConn.Close()
		_ = remoteConn.Close()
		copyDone <- struct{}{}
	}()
	<-copyDone
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
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
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
		schema := provisionSchemaScope{
			Schema:    cleanSimpleIdentifierValue(stringInput(schemaMap, "schema")),
			AllTables: boolInput(schemaMap, "all_tables"),
		}
		if schema.Schema == "" {
			return provisionScope{}, fmt.Errorf("scope schema is required and must be a simple identifier")
		}
		if !schema.AllTables {
			for _, tableItem := range anySlice(schemaMap["tables"]) {
				tableMap, ok := tableItem.(map[string]any)
				if !ok {
					return provisionScope{}, fmt.Errorf("scope tables must be objects")
				}
				table := provisionTableScope{
					Table:      cleanSimpleIdentifierValue(stringInput(tableMap, "table")),
					AllColumns: boolInput(tableMap, "all_columns"),
				}
				if table.Table == "" {
					return provisionScope{}, fmt.Errorf("scope table is required and must be a simple identifier")
				}
				if !table.AllColumns {
					for _, column := range stringSlice(tableMap["columns"]) {
						clean := cleanSimpleIdentifierValue(column)
						if clean == "" {
							return provisionScope{}, fmt.Errorf("scope column is required and must be a simple identifier")
						}
						table.Columns = append(table.Columns, clean)
					}
					if len(table.Columns) == 0 {
						return provisionScope{}, fmt.Errorf("selected table must grant all columns or at least one column")
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
		statements = append(statements, fmt.Sprintf(`
DO $$
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
$$`, quoteLiteral(roleName), privileges, quoteLiteral(roleName)))
		return statements, map[string]any{"preset": preset, "database": database, "grants": grants}, nil
	}
	for _, schema := range scope.Schemas {
		schemaSQL := quoteIdentifier(schema.Schema)
		statements = append(statements, fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", schemaSQL, roleSQL))
		if schema.AllTables {
			statements = append(statements, fmt.Sprintf("GRANT %s ON ALL TABLES IN SCHEMA %s TO %s", privileges, schemaSQL, roleSQL))
			grants = append(grants, map[string]any{"schema": schema.Schema, "all_tables": true, "privileges": privileges})
			continue
		}
		for _, table := range schema.Tables {
			if !table.AllColumns && preset == "read_write" {
				return nil, nil, fmt.Errorf("column-scoped read/write grants are not supported; choose all columns for write access")
			}
			if table.AllColumns {
				statements = append(statements, fmt.Sprintf("GRANT %s ON TABLE %s TO %s", privileges, qualifiedIdentifierSQL(schema.Schema, table.Table), roleSQL))
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
	return statements, map[string]any{"preset": preset, "database": database, "grants": grants}, nil
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

type limitedBuffer struct {
	Limit int
	data  bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Limit <= 0 {
		return len(p), nil
	}
	remaining := b.Limit - b.data.Len()
	if remaining <= 0 {
		return 0, fmt.Errorf("postgres command output exceeded %d bytes", b.Limit)
	}
	if len(p) > remaining {
		_, _ = b.data.Write(p[:remaining])
		return remaining, fmt.Errorf("postgres command output exceeded %d bytes", b.Limit)
	}
	return b.data.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.data.Bytes()
}

func (b *limitedBuffer) String() string {
	return b.data.String()
}

func postgresCommandError(command string, err error, stderr string) error {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return fmt.Errorf("%s is not available in the gateway container; rebuild with postgresql-client installed", command)
	}
	message := strings.TrimSpace(truncateUTF8Bytes(stderr, 4000))
	if message == "" {
		message = err.Error()
	}
	if errors.Is(err, io.ErrShortWrite) {
		message = "command output exceeded gateway limit"
	}
	if command == "pg_dump" && strings.Contains(message, "server version mismatch") {
		message += "\nThe gateway pg_dump client is older than this Postgres server. Rebuild the AIPermission backend image so the bundled Postgres client is updated."
	}
	return fmt.Errorf("%s failed: %s", command, message)
}

func postgresSafeFilename(parts ...string) string {
	candidate := strings.Join(parts, "-")
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	candidate = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(candidate, "-")
	candidate = strings.Trim(candidate, "-._")
	if candidate == "" {
		return "postgres-backup"
	}
	return candidate
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
		return "require"
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
	checkSQL := readonlyValidationSQL(normalized)
	if strings.Contains(checkSQL, ";") {
		return fmt.Errorf("%s only accepts a single statement", ActionQueryReadonly)
	}
	if disallowedReadonlyTerms.MatchString(checkSQL) {
		return fmt.Errorf("%s only accepts read-only SQL", ActionQueryReadonly)
	}
	if !hasReadonlyPrefix(strings.TrimSpace(checkSQL)) {
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

func readonlyValidationSQL(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))
	for i := 0; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "--"):
			for i < len(sql) && sql[i] != '\n' {
				out.WriteByte(' ')
				i++
			}
		case strings.HasPrefix(sql[i:], "/*"):
			out.WriteString("  ")
			i += 2
			for i < len(sql) && !strings.HasPrefix(sql[i:], "*/") {
				if sql[i] == '\n' {
					out.WriteByte('\n')
				} else {
					out.WriteByte(' ')
				}
				i++
			}
			if strings.HasPrefix(sql[i:], "*/") {
				out.WriteString("  ")
				i += 2
			}
		case sql[i] == '\'':
			i = maskQuotedSQL(sql, i, '\'', &out)
		case sql[i] == '"':
			i = maskQuotedSQL(sql, i, '"', &out)
		case sql[i] == '$':
			if end := dollarQuoteEnd(sql, i); end > i {
				for i < end {
					out.WriteByte(' ')
					i++
				}
			} else {
				out.WriteByte(byteLower(sql[i]))
				i++
			}
		default:
			out.WriteByte(byteLower(sql[i]))
			i++
		}
	}
	return out.String()
}

func maskQuotedSQL(sql string, start int, quote byte, out *strings.Builder) int {
	i := start
	if i < len(sql) {
		out.WriteByte(' ')
		i++
	}
	for i < len(sql) {
		if sql[i] == '\n' {
			out.WriteByte('\n')
		} else {
			out.WriteByte(' ')
		}
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == quote {
				i += 2
				out.WriteByte(' ')
				continue
			}
			i++
			break
		}
		i++
	}
	return i
}

func dollarQuoteEnd(sql string, start int) int {
	if sql[start] != '$' {
		return -1
	}
	next := strings.IndexByte(sql[start+1:], '$')
	if next < 0 {
		return -1
	}
	tagEnd := start + 1 + next
	tag := sql[start : tagEnd+1]
	if !validDollarQuoteTag(tag) {
		return -1
	}
	closing := strings.Index(sql[tagEnd+1:], tag)
	if closing < 0 {
		return -1
	}
	return tagEnd + 1 + closing + len(tag)
}

func validDollarQuoteTag(tag string) bool {
	if len(tag) < 2 || tag[0] != '$' || tag[len(tag)-1] != '$' {
		return false
	}
	body := tag[1 : len(tag)-1]
	for i := 0; i < len(body); i++ {
		ch := body[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}

func byteLower(ch byte) byte {
	if ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
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
