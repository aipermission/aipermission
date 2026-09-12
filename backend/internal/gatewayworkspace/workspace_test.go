package gatewayworkspace

import (
	"database/sql"
	"testing"

	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
	runtimeidentity "github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation/identity"
)

func TestComposeRuntimePublishesNarrowCapabilitiesWithoutExposingConcreteOwner(t *testing.T) {
	database := &sql.DB{}
	secretVault, err := vault.New("gateway-secret")
	if err != nil {
		t.Fatal(err)
	}
	tokenStore := tokens.NewStore(database)
	registry := connectorcatalog.NewRegistry()
	owner := workspaceruntime.New(foundation.State{
		ID: "database-one", Path: "/data/database-one.aipdb", Database: database,
		Registry: registry, AdapterRegistry: connectorapi.NewRegistry(), TokenStore: tokenStore,
		Identity: runtimeidentity.State{
			GatewaySecret: "gateway-secret", WorkspaceUUID: "workspace-one",
			RuntimeInstanceID: "runtime-one", UIRetryIdentity: "retry-one",
			ActionIdentityKey: []byte("action-identity"), Vault: secretVault,
		},
	})

	runtime, err := composeRuntime(owner)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Identity.DatabaseID != owner.ID || runtime.Identity.RuntimeID != owner.RuntimeInstanceID {
		t.Fatalf("runtime identity = %#v", runtime.Identity)
	}
	action, ok := runtime.ConnectorActionCapability()
	if !ok || action.Database != database || action.Tokens != tokenStore || action.Registry != registry || action.Vault != secretVault {
		t.Fatalf("connector action capability = %#v", action)
	}
	access, ok := runtime.AccessControlCapability()
	if !ok || access.Database != database || access.Tokens != tokenStore || access.Registry != registry || access.Policy == nil || access.Delivery == nil {
		t.Fatalf("access control capability = %#v", access)
	}
	observation, ok := runtime.ObservationCapability()
	if !ok || observation.Database != database || observation.Registry != registry || observation.PrepareRedactor == nil ||
		observation.AuditDispatcher == nil || observation.SetAuditDispatcher == nil || observation.RetentionService == nil || observation.SetRetentionService == nil {
		t.Fatalf("observation capability = %#v", observation)
	}
	dispatcher := observability.NewDispatcher(database)
	observation.SetAuditDispatcher(dispatcher)
	if observation.AuditDispatcher() != dispatcher {
		t.Fatal("observation capability did not preserve dispatcher ownership")
	}
	retentionService := retention.NewService(database, "workspace-one")
	observation.SetRetentionService(retentionService)
	if observation.RetentionService() != retentionService {
		t.Fatal("observation capability did not preserve retention ownership")
	}
	if source, ok := runtime.MCPTokenSourceCapability(); !ok || source != tokenStore {
		t.Fatal("MCP token capability did not project the authentication port")
	}
	if backup, ok := runtime.BackupCapability(); !ok || backup.Database != database || backup.Vault != secretVault {
		t.Fatalf("backup capability = %#v", backup)
	}
	if transport, ok := runtime.ConnectorTransportCapability(); !ok || transport.Database != database || transport.Scopes == nil || transport.Delivery == nil {
		t.Fatalf("connector transport capability = %#v", transport)
	}
	if _, ok := runtime.ConsoleRecoveryCapability(); ok {
		t.Fatal("console recovery capability was published before console runtime configuration")
	}
	configuration, ok := runtime.RuntimeConfigurationCapability()
	if !ok || configuration.ConfigureConsole == nil {
		t.Fatal("runtime configuration capability is unavailable")
	}
	configuration.ConfigureConsole(nil, nil)
	_, ok = runtime.ConsoleRecoveryCapability()
	if !ok {
		t.Fatal("console recovery capability was not published after console runtime configuration")
	}
	command, commandOK := runtime.CommandCapability()
	live, liveOK := runtime.LiveConsoleCapability()
	vaultSession, vaultOK := runtime.VaultSessionCapability()
	if !commandOK || !liveOK || !vaultOK || command.Sessions == nil ||
		live.Sessions != command.Sessions || vaultSession.Sessions != command.Sessions {
		t.Fatal("session capabilities did not preserve one workspace-owned session manager")
	}
}

func TestRuntimeCapabilitiesFailClosedWithoutConcreteOwner(t *testing.T) {
	runtime := &Runtime{Identity: RuntimeIdentity{DatabaseID: "detached"}}
	checks := map[string]func() bool{
		"access control":       func() bool { _, ok := runtime.AccessControlCapability(); return ok },
		"connector action":     func() bool { _, ok := runtime.ConnectorActionCapability(); return ok },
		"connector management": func() bool { _, ok := runtime.ConnectorManagementCapability(); return ok },
		"connector transport":  func() bool { _, ok := runtime.ConnectorTransportCapability(); return ok },
		"observation":          func() bool { _, ok := runtime.ObservationCapability(); return ok },
		"command":              func() bool { _, ok := runtime.CommandCapability(); return ok },
		"backup":               func() bool { _, ok := runtime.BackupCapability(); return ok },
		"vault":                func() bool { _, ok := runtime.VaultRuntimeCapability(); return ok },
		"security":             func() bool { _, ok := runtime.SecurityPolicyCapability(); return ok },
	}
	for name, available := range checks {
		if available() {
			t.Errorf("%s capability was published without a concrete owner", name)
		}
	}
}

func TestCloseClearsCompositionAndOwnerActionIdentity(t *testing.T) {
	owner := &workspaceruntime.Runtime{ID: "database-one", ActionIdentityKey: []byte("action-identity")}
	runtime := &Runtime{owner: owner}

	if err := (&Component{}).Close(runtime, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if owner.HasActionIdentity() {
		t.Fatal("workspace close retained action identity material")
	}
}
