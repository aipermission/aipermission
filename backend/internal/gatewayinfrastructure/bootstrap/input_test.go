package bootstrap

import (
	"database/sql"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestAdoptWorkspaceInputPreservesConstructionDependencies(t *testing.T) {
	database := &sql.DB{}
	secretVault := &vault.Vault{}
	tokenStore := &tokens.Store{}
	registry := connectors.NewRegistry()
	adapters := connectorapi.NewRegistry()
	runtimeID := func() (string, error) { return "runtime", nil }

	got := (Adopt{
		ID: "database", Path: "/data/database.aipdb", ConfiguredGatewaySecret: "secret",
		Database: database, Vault: secretVault, TokenStore: tokenStore,
		Registry: registry, AdapterRegistry: adapters, RuntimeInstanceID: runtimeID,
	}).WorkspaceInput()

	if got.ID != "database" || got.Path != "/data/database.aipdb" || got.ConfiguredGatewaySecret != "secret" {
		t.Fatalf("identity fields were not preserved: %#v", got)
	}
	if got.Database != database || got.Vault != secretVault || got.TokenStore != tokenStore || got.Registry != registry || got.AdapterRegistry != adapters {
		t.Fatal("construction dependency identity was not preserved")
	}
	if id, err := got.RuntimeInstanceID(); err != nil || id != "runtime" {
		t.Fatalf("runtime id = %q, %v", id, err)
	}
}

func TestOpenWorkspaceInputPreservesConstructionDependencies(t *testing.T) {
	registry := connectors.NewRegistry()
	adapters := connectorapi.NewRegistry()
	got := (Open{
		ID: "database", Path: "/data/database.aipdb", Password: "password",
		ConfiguredGatewaySecret: "secret", Registry: registry, AdapterRegistry: adapters,
	}).WorkspaceInput()

	if got.ID != "database" || got.Path != "/data/database.aipdb" || got.Password != "password" || got.ConfiguredGatewaySecret != "secret" {
		t.Fatalf("open fields were not preserved: %#v", got)
	}
	if got.Registry != registry || got.AdapterRegistry != adapters {
		t.Fatal("registry dependency identity was not preserved")
	}
}
