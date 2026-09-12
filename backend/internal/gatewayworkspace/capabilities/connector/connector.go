package connector

import (
	"database/sql"

	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type ActionCapability struct {
	Database *sql.DB
	Tokens   *tokens.Store
	Registry connectorcatalog.Catalog
	Vault    *vault.Vault
	Policy   *securitypolicy.Service
	Delivery *vaultsessions.DeliveryCoordinator
	Control  *runtimecontrol.State
}

type ApprovalCapability struct {
	Database *sql.DB
	Control  *runtimecontrol.State
}

type CatalogCapability struct {
	Database *sql.DB
	Registry connectorcatalog.Catalog
}

type CredentialCapability struct{ Vault *vault.Vault }

type ManagementCapability struct {
	Database *sql.DB
	Registry connectorcatalog.Catalog
	Delivery *vaultsessions.DeliveryCoordinator
}

type TransportCapability struct {
	Scopes   connectortransport.ScopeRuntime
	Database *sql.DB
	Delivery *vaultsessions.DeliveryCoordinator
}

func NewAction(database *sql.DB, tokenStore *tokens.Store, registry connectorcatalog.Catalog, secretVault *vault.Vault, policy *securitypolicy.Service, delivery *vaultsessions.DeliveryCoordinator, control *runtimecontrol.State) ActionCapability {
	return ActionCapability{Database: database, Tokens: tokenStore, Registry: registry, Vault: secretVault, Policy: policy, Delivery: delivery, Control: control}
}

func NewApproval(database *sql.DB, control *runtimecontrol.State) ApprovalCapability {
	return ApprovalCapability{Database: database, Control: control}
}

func NewCatalog(database *sql.DB, registry connectorcatalog.Catalog) CatalogCapability {
	return CatalogCapability{Database: database, Registry: registry}
}

func NewCredential(secretVault *vault.Vault) CredentialCapability {
	return CredentialCapability{Vault: secretVault}
}

func NewManagement(database *sql.DB, registry connectorcatalog.Catalog, delivery *vaultsessions.DeliveryCoordinator) ManagementCapability {
	return ManagementCapability{Database: database, Registry: registry, Delivery: delivery}
}

func NewTransport(scopes connectortransport.ScopeRuntime, database *sql.DB, delivery *vaultsessions.DeliveryCoordinator) TransportCapability {
	return TransportCapability{Scopes: scopes, Database: database, Delivery: delivery}
}
