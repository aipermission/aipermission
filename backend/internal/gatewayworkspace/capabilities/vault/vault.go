package vault

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	secretvault "github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type RuntimeCapability struct {
	Database *sql.DB
	Vault    *secretvault.Vault
	Tokens   *tokens.Store
	Sessions *console.Manager
	Leases   *vaultsessions.Store
	Delivery *vaultsessions.DeliveryCoordinator
	Control  *runtimecontrol.State
	Policy   *securitypolicy.Service
}

type SessionCapability struct {
	Database             *sql.DB
	Sessions             *console.Manager
	Leases               *vaultsessions.Store
	Delivery             *vaultsessions.DeliveryCoordinator
	InstallAuthorizer    func(func(context.Context, func() error, func() error) error)
	InstallSessionClosed func(func(context.Context, int64, int64, int64) error)
}

type MCPCapability struct {
	Database *sql.DB
	Vault    *secretvault.Vault
	Control  *runtimecontrol.State
}

type ApprovalCapability struct{ Control *runtimecontrol.State }

func NewRuntime(database *sql.DB, vault *secretvault.Vault, tokenStore *tokens.Store, sessions *console.Manager, leases *vaultsessions.Store, delivery *vaultsessions.DeliveryCoordinator, control *runtimecontrol.State, policy *securitypolicy.Service) RuntimeCapability {
	return RuntimeCapability{Database: database, Vault: vault, Tokens: tokenStore, Sessions: sessions, Leases: leases, Delivery: delivery, Control: control, Policy: policy}
}

func NewSession(database *sql.DB, sessions *console.Manager, leases *vaultsessions.Store, delivery *vaultsessions.DeliveryCoordinator, configureAuthorizer func(*vaultsessions.Store, func(context.Context, func() error, func() error) error), installSessionClosed func(func(context.Context, int64, int64, int64) error)) SessionCapability {
	installAuthorizer := func(guard func(context.Context, func() error, func() error) error) {
		configureAuthorizer(leases, guard)
	}
	return SessionCapability{
		Database: database, Sessions: sessions, Leases: leases, Delivery: delivery,
		InstallAuthorizer: installAuthorizer, InstallSessionClosed: installSessionClosed,
	}
}

func NewMCP(database *sql.DB, vault *secretvault.Vault, control *runtimecontrol.State) MCPCapability {
	return MCPCapability{Database: database, Vault: vault, Control: control}
}

func NewApproval(control *runtimecontrol.State) ApprovalCapability {
	return ApprovalCapability{Control: control}
}
