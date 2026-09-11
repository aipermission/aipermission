// Package gatewayinfrastructure exposes process, routing, and workspace composition contracts to the API boundary.
package gatewayinfrastructure

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

var ErrInitialization = gatewayworkspace.ErrInitialization

// Runtime is the only unlocked-workspace contract exposed to the API
// composition boundary. Implementations remain owned by gatewayworkspace.
type Runtime interface {
	gatewayworkspace.Runtime
}
type WorkspaceLifecyclePort interface{ gatewayworkspace.LifecyclePort }
type ActionWorkflow = gatewayworkspace.ActionWorkflow
type CommandWorkflow = gatewayworkspace.CommandWorkflow
type TransferWorkflow = gatewayworkspace.TransferWorkflow
type AdoptInput = gatewayworkspace.AdoptInput

type WorkspaceDependencies struct {
	DataPath              string
	Open                  func(string, string, string) (Runtime, error)
	Close                 func(Runtime) error
	OnActivated, OnOpened func(Runtime)
	Move                  func(string, string) error
	Delete                func(string) error
	ValidateNewPassword   func(context.Context, *sql.DB, string, string) error
	Publish               func(string, string) error
	GatewaySecret         func() string
}

type WorkspaceHTTPDependencies = gatewayworkspace.HTTPDependencies
type WorkspaceHTTPHandlers = gatewayworkspace.HTTPHandlers
type Identity = gatewayworkspace.Identity
type OpenInput = gatewayworkspace.OpenInput
type PasswordAttempt = gatewayworkspace.PasswordAttempt
type TokenStore = gatewayworkspace.TokenStore
type Vault = gatewayworkspace.Vault
