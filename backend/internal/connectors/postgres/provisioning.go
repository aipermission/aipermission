package postgresconnector

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5"
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
	marker, err := randomProvisionMarker()
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	statements = append(statements, fmt.Sprintf("COMMENT ON ROLE %s IS %s", quoteIdentifier(roleName), quoteLiteral(marker)))

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	conn, err := connect(ctx, runtime)
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
	if commitErr := tx.Commit(ctx); commitErr != nil {
		committed, verifyErr := verifyProvisionedRole(ctx, runtime, roleName, marker)
		if !committed {
			return connectors.ProvisionedCredentialProfile{}, connectors.ClassifyOutcomeUnknown(
				"transaction_commit",
				map[string]any{"reconciliation_required": true},
				errors.Join(fmt.Errorf("commit postgres role provisioning: %w", commitErr), verifyErr),
			)
		}
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

func randomProvisionMarker() (string, error) {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate provisioning marker: %w", err)
	}
	return "aipermission-provision:" + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func verifyProvisionedRole(ctx context.Context, runtime connectors.RuntimeContext, roleName, marker string) (bool, error) {
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	conn, err := connect(verifyCtx, runtime)
	if err != nil {
		return false, fmt.Errorf("reconnect to verify postgres role provisioning: %w", err)
	}
	defer conn.Close(context.Background())
	var comment string
	err = conn.QueryRow(verifyCtx, `
		SELECT COALESCE(shobj_description(oid, 'pg_authid'), '')
		FROM pg_catalog.pg_roles WHERE rolname = $1`, roleName).Scan(&comment)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errors.New("postgres role was not found after an ambiguous commit")
	}
	if err != nil {
		return false, fmt.Errorf("verify postgres role provisioning marker: %w", err)
	}
	if comment != marker {
		return false, errors.New("postgres role provisioning marker did not match after an ambiguous commit")
	}
	return true, nil
}

func (Connector) PreserveProvisionedCredentialPublic(existing connectors.CredentialProfileView, requested map[string]any) (map[string]any, error) {
	if requested == nil {
		requested = map[string]any{}
	}
	if !boolPublic(existing.Public, "managed_by_aipermission") {
		return clonePublicMap(requested), nil
	}
	next := clonePublicMap(requested)
	existingUsername := strings.TrimSpace(publicString(existing.Public, "username"))
	requestedUsername := strings.TrimSpace(publicString(next, "username"))
	if requestedUsername != "" && existingUsername != "" && requestedUsername != existingUsername {
		return nil, fmt.Errorf("managed credential username cannot be changed; create a new managed profile instead")
	}
	if existingUsername != "" {
		next["username"] = existingUsername
	}
	for _, key := range []string{
		"managed_by_aipermission",
		"managed_role_name",
		"managed_admin_profile_id",
		"managed_admin_profile_ref",
		"managed_preset",
		"managed_scope",
	} {
		if value, ok := existing.Public[key]; ok {
			next[key] = value
		}
	}
	return next, nil
}

func (Connector) ProvisionedCredentialAdminProfileID(profile connectors.CredentialProfileView) (int64, bool, error) {
	if !boolPublic(profile.Public, "managed_by_aipermission") {
		return 0, false, nil
	}
	adminProfileID := int64Public(profile.Public, "managed_admin_profile_id")
	if adminProfileID < 1 || adminProfileID == profile.ID {
		return 0, true, fmt.Errorf("managed credential profile is missing a valid admin profile reference")
	}
	return adminProfileID, true, nil
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
	conn, err := connect(ctx, runtime)
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
			Status: connectors.ResultCompleted,
			Output: map[string]any{
				"role_name":                  roleName,
				"dropped":                    false,
				"reason":                     "role not found",
				"ownership_reassigned":       false,
				"ownership_reassigned_to":    adminRole,
				"managed_privileges_removed": false,
			},
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
	if commitErr := tx.Commit(ctx); commitErr != nil {
		absent, verifyErr := verifyPostgresRoleAbsent(ctx, runtime, roleName)
		if !absent {
			return connectors.ActionResult{}, connectors.ClassifyOutcomeUnknown(
				"transaction_commit",
				map[string]any{"reconciliation_required": true},
				errors.Join(fmt.Errorf("commit postgres role cleanup: %w", commitErr), verifyErr),
			)
		}
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"role_name":                  roleName,
			"dropped":                    true,
			"ownership_reassigned":       true,
			"ownership_reassigned_to":    adminRole,
			"managed_privileges_removed": true,
		},
		DisplayText: "Reassigned owned objects and dropped managed Postgres role",
	}, nil
}

func verifyPostgresRoleAbsent(ctx context.Context, runtime connectors.RuntimeContext, roleName string) (bool, error) {
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	conn, err := connect(verifyCtx, runtime)
	if err != nil {
		return false, fmt.Errorf("reconnect to verify postgres role cleanup: %w", err)
	}
	defer conn.Close(context.Background())
	var exists bool
	if err := conn.QueryRow(verifyCtx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, roleName).Scan(&exists); err != nil {
		return false, fmt.Errorf("verify postgres role cleanup: %w", err)
	}
	if exists {
		return false, errors.New("postgres role still exists after an ambiguous cleanup commit")
	}
	return true, nil
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
