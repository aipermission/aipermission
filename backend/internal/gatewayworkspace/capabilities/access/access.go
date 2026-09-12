package access

import (
	"context"
	"database/sql"
	"time"

	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type MCPTokenSource interface {
	AuthenticateHash(context.Context, string, time.Time) (tokens.Authentication, error)
}

type VaultMetadataCapability struct{ Database *sql.DB }

type AccessControlCapability struct {
	Database *sql.DB
	Tokens   *tokens.Store
	Registry *connectorcatalog.Registry
	Policy   *securitypolicy.Service
	Delivery *vaultsessions.DeliveryCoordinator
}

type MCPReadCapability struct {
	Database *sql.DB
	Registry *connectorcatalog.Registry
}

type MCPActionCapability struct {
	Database *sql.DB
	Tokens   *tokens.Store
	Leases   *vaultsessions.Store
	Delivery *vaultsessions.DeliveryCoordinator
	Control  *runtimecontrol.State
}

type MCPRuntimeCapability struct {
	Control  *runtimecontrol.State
	Delivery *vaultsessions.DeliveryCoordinator
}

type RuntimeControlCapability struct {
	MCPStarted    func() bool
	SetMCPStarted func(bool)
}

type ConsoleRecoveryCapability struct {
	Recover func(context.Context, executionprincipal.Principal, int64, func() error) ([]int64, error)
}

type SecurityPolicyCapability struct{ Policy *securitypolicy.Service }

type RuntimeConfigurationCapability struct {
	Policy           *securitypolicy.Service
	Control          *runtimecontrol.State
	ConfigureConsole func(console.RuntimeOpener, func(string) string)
}

type ConsoleConfigurationCapability struct {
	Configure func(console.RuntimeOpener, func(string) string)
}

type MessageCapability struct{ Database *sql.DB }

type ProjectCapability struct {
	Database *sql.DB
	Delivery *vaultsessions.DeliveryCoordinator
}

func NewVaultMetadata(database *sql.DB) VaultMetadataCapability {
	return VaultMetadataCapability{Database: database}
}

func NewAccessControl(database *sql.DB, tokenStore *tokens.Store, registry *connectorcatalog.Registry, policy *securitypolicy.Service, delivery *vaultsessions.DeliveryCoordinator) AccessControlCapability {
	return AccessControlCapability{Database: database, Tokens: tokenStore, Registry: registry, Policy: policy, Delivery: delivery}
}

func NewMCPRead(database *sql.DB, registry *connectorcatalog.Registry) MCPReadCapability {
	return MCPReadCapability{Database: database, Registry: registry}
}

func NewMCPAction(database *sql.DB, tokenStore *tokens.Store, leases *vaultsessions.Store, delivery *vaultsessions.DeliveryCoordinator, control *runtimecontrol.State) MCPActionCapability {
	return MCPActionCapability{Database: database, Tokens: tokenStore, Leases: leases, Delivery: delivery, Control: control}
}

func NewMCPRuntime(control *runtimecontrol.State, delivery *vaultsessions.DeliveryCoordinator) MCPRuntimeCapability {
	return MCPRuntimeCapability{Control: control, Delivery: delivery}
}

func NewRuntimeControl(control *runtimecontrol.State) RuntimeControlCapability {
	return RuntimeControlCapability{MCPStarted: control.MCPStarted, SetMCPStarted: control.SetMCPStarted}
}

func NewConsoleRecovery(sessions *console.Manager) ConsoleRecoveryCapability {
	return ConsoleRecoveryCapability{Recover: sessions.RecoverRuntime}
}

func NewSecurityPolicy(policy *securitypolicy.Service) SecurityPolicyCapability {
	return SecurityPolicyCapability{Policy: policy}
}

func NewRuntimeConfiguration(policy *securitypolicy.Service, control *runtimecontrol.State, configure func(console.RuntimeOpener, func(string) string)) RuntimeConfigurationCapability {
	return RuntimeConfigurationCapability{Policy: policy, Control: control, ConfigureConsole: configure}
}

func NewConsoleConfiguration(configure func(console.RuntimeOpener, func(string) string)) ConsoleConfigurationCapability {
	return ConsoleConfigurationCapability{Configure: configure}
}

func NewMessage(database *sql.DB) MessageCapability { return MessageCapability{Database: database} }

func NewProject(database *sql.DB, delivery *vaultsessions.DeliveryCoordinator) ProjectCapability {
	return ProjectCapability{Database: database, Delivery: delivery}
}
