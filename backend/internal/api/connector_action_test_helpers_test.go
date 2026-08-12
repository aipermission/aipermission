package api

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func openAPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func connectorActionTestRuntime(t *testing.T, database *sql.DB, secretVault *vault.Vault) *databaseRuntime {
	t.Helper()
	runtime := &databaseRuntime{
		database: database,
		vault:    secretVault,
		tokens:   tokens.NewStore(database),
		registry: testConnectorRegistry(t),
	}
	runtime.setMCPStarted(true)
	return runtime
}

func openAPITestVault(t *testing.T) *vault.Vault {
	t.Helper()
	secretVault, err := vault.New("test-gateway-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	return secretVault
}

func createAPITestPostgresTargetProfile(t *testing.T, store *connectortargets.Store, secretVault *vault.Vault) (connectortargets.Target, connectortargets.CredentialProfile) {
	t.Helper()
	ctx := context.Background()
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: postgresconnector.Kind,
		Name:          "main-db",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5432,
			"database":        "app",
			"ssl_mode":        "disable",
		},
	})
	if err != nil {
		t.Fatalf("create postgres target: %v", err)
	}
	encryptedSecret, err := secretVault.EncryptJSON(map[string]any{"password": "secret"})
	if err != nil {
		t.Fatalf("encrypt profile secret: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       postgresconnector.Kind,
		Kind:                "username_password",
		Label:               "readonly",
		Public:              map[string]any{"username": "app_readonly"},
		EncryptedSecretJSON: encryptedSecret,
	})
	if err != nil {
		t.Fatalf("create postgres profile: %v", err)
	}
	return target, profile
}

func insertAPITestToken(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('connector-codex', 'connector-hash', 'aip_conn', ?, ?)`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("token id: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
		SELECT ?, id, 1, ?, ? FROM projects WHERE status = 'active'`, id, now, now); err != nil {
		t.Fatalf("insert token project scopes: %v", err)
	}
	return id
}

const localActionTestConnectorKind = "localtest"

type localActionTestConnector struct{}

func (localActionTestConnector) ValidateTargetConfig(config map[string]any) error {
	if value, _ := config["semantic_error"].(bool); value {
		return fmt.Errorf("semantic validation fixture")
	}
	return nil
}

func (localActionTestConnector) Kind() string {
	return localActionTestConnectorKind
}

func (localActionTestConnector) Label() string {
	return "Local Test"
}

func (localActionTestConnector) Version() string {
	return "0.1"
}

func (localActionTestConnector) TargetSchema() connectors.Schema {
	return connectors.Schema{}
}

func (localActionTestConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{
		Kind:  "default",
		Label: "Default",
		Schema: connectors.Schema{Fields: []connectors.Field{{
			Name:   "password",
			Label:  "Password",
			Type:   connectors.FieldSecret,
			Secret: true,
		}}},
	}}
}

func (localActionTestConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{
		Title:       "Local test target",
		Summary:     "Test connector.",
		Connector:   "Local Test",
		ConnectorID: localActionTestConnectorKind,
	}, nil
}

func (localActionTestConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{{
		Name:        "echo",
		Label:       "Echo",
		Description: "Echo one value.",
		Risk:        connectors.RiskRead,
		InputSchema: connectors.Schema{Fields: []connectors.Field{{
			Name:     "value",
			Label:    "Value",
			Type:     connectors.FieldString,
			Required: true,
		}, {
			Name:    "mode",
			Label:   "Mode",
			Type:    connectors.FieldString,
			Default: "safe",
		}}},
	}}, nil
}

func (localActionTestConnector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	value, _ := req.Input["value"].(string)
	return connectors.PreparedAction{
		ConnectorKind: localActionTestConnectorKind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Risk:          connectors.RiskRead,
		Title:         "Echo value",
		Summary:       "Echo one test value",
		Preview:       map[string]any{"value": value},
		Payload:       map[string]any{"value": value},
		ContextMaterial: map[string]any{
			"value": value,
		},
	}, nil
}

func (localActionTestConnector) ExecuteAction(_ context.Context, _ connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if action.Payload["value"] == "classified-error" {
		return connectors.ActionResult{}, connectors.ClassifyError("fixture_failure", fmt.Errorf("fixture failed"))
	}
	result := connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"echo": action.Payload["value"]},
		DisplayText: fmt.Sprint(action.Payload["value"]),
	}
	if action.Payload["value"] == "with-handle" {
		result.Handles = connectors.ActionHandles{SessionID: 123, SessionGeneration: 456}
	}
	if action.Payload["value"] == "incomplete-handle" {
		result.Handles = connectors.ActionHandles{SessionID: 123}
	}
	return result, nil
}
