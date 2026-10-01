package postgresconnector

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

func (Connector) ProvisionCredentialProfile(ctx context.Context, runtime connectors.RuntimeContext, input map[string]any) (connectors.ProvisionedCredentialProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
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
	journal, err := rolejournal.FromRuntime(runtime)
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	authority, err := rolejournal.Authority(runtime)
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	password, err := randomCredentialPassword()
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	if registrar, ok := runtime.Secrets.(connectors.SensitiveValueRegistrar); ok {
		registrar.RegisterSensitiveValue(password)
	}
	statements, summary, err := provisionRoleStatements(runtime.Target, roleName, password, preset, scope)
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	entry, err := rolecatalog.Provision(ctx, journal, authority, roleName, statements, managedRoleDial(runtime))
	if err != nil {
		return connectors.ProvisionedCredentialProfile{}, err
	}
	public := map[string]any{
		"username": roleName, "managed_by_aipermission": true, "managed_role_name": roleName,
		"managed_admin_profile_id": runtime.Profile.ID, "managed_admin_profile_ref": runtime.Profile.Label,
		"managed_preset": preset, "managed_scope": summary, "managed_identity": entry.Reference(),
	}
	return connectors.ProvisionedCredentialProfile{
		Kind: "username_password", Label: profileLabel, Public: public,
		Secret: map[string]any{"password": password}, RiskLabel: provisionRiskLabel(preset),
		Result: connectors.ActionResult{Status: connectors.ResultCompleted,
			Output:      map[string]any{"role_name": roleName, "profile_label": profileLabel, "preset": preset, "scope": summary},
			DisplayText: "Created Postgres role and saved credential profile"},
	}, nil
}

func managedRoleDial(runtime connectors.RuntimeContext) rolecatalog.Dial {
	return func(ctx context.Context) (rolecatalog.Connection, error) {
		connection, err := connect(ctx, runtime)
		if err != nil {
			return nil, err
		}
		return connection, nil
	}
}
