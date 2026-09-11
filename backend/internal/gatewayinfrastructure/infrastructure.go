// Package gatewayinfrastructure exposes process, routing, and workspace composition contracts to the API boundary.
package gatewayinfrastructure

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	connectorstate "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/connectors"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/observation"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/security"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
)

func InitializationError() error { return gatewayworkspace.InitializationError() }

type WorkspaceLifecyclePort interface{ gatewayworkspace.LifecyclePort }

type RuntimeIdentity struct {
	DatabaseID   string
	DatabasePath string
	WorkspaceID  string
	RuntimeID    string
	UIRetryID    string
}

func (identity RuntimeIdentity) Ready() bool {
	return identity.WorkspaceID != "" && identity.RuntimeID != ""
}

// Runtime is the infrastructure-owned composition handle exposed to the API.
// The workspace owner remains opaque so lifecycle internals cannot leak across
// the transport boundary.
type Runtime struct {
	Identity    RuntimeIdentity
	Storage     storage.Port
	Connectors  connectorstate.Port
	Security    security.Port
	Observation observation.Port
	owner       *gatewayworkspace.Runtime
}

func wrapRuntime(owner *gatewayworkspace.Runtime) *Runtime {
	if owner == nil {
		return nil
	}
	identity := owner.Identity
	return &Runtime{
		Identity: RuntimeIdentity{
			DatabaseID: identity.DatabaseID, DatabasePath: identity.DatabasePath,
			WorkspaceID: identity.WorkspaceID, RuntimeID: identity.RuntimeID, UIRetryID: identity.UIRetryID,
		},
		Storage: owner.Storage, Connectors: owner.Connectors, Security: owner.Security, Observation: owner.Observation,
		owner: owner,
	}
}

func (runtime *Runtime) WorkspaceIdentity() Identity {
	if runtime == nil {
		return Identity{}
	}
	return Identity{ID: runtime.Identity.DatabaseID, Path: runtime.Identity.DatabasePath, RetryIdentity: runtime.Identity.UIRetryID}
}

func (runtime *Runtime) ConfiguredGatewaySecret() string {
	if runtime == nil || runtime.owner == nil {
		return ""
	}
	return runtime.owner.ConfiguredGatewaySecret()
}

func (runtime *Runtime) TagActionIdentity(canonical []byte) (string, error) {
	if runtime == nil || runtime.owner == nil {
		return "", InitializationError()
	}
	return runtime.owner.TagActionIdentity(canonical)
}

type ActionWorkflow = gatewayworkspace.ActionWorkflow
type CommandWorkflow = gatewayworkspace.CommandWorkflow
type TransferWorkflow = gatewayworkspace.TransferWorkflow
type AdoptInput = gatewayworkspace.AdoptInput

type WorkspaceDependencies struct {
	DataPath              string
	Open                  func(string, string, string) (*Runtime, error)
	Close                 func(*Runtime) error
	OnActivated, OnOpened func(*Runtime)
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
