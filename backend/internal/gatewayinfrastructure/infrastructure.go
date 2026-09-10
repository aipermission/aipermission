// Package gatewayinfrastructure exposes process, routing, and workspace composition contracts to the API boundary.
package gatewayinfrastructure

import (
	"github.com/aipermission/aipermission/backend/internal/gatewayoptions"
	"github.com/aipermission/aipermission/backend/internal/gatewayroutes"
	"github.com/aipermission/aipermission/backend/internal/gatewaystate"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

var (
	ResolveOptions                 = gatewayoptions.Resolve
	WithConnectorAdapterRegistry   = gatewayoptions.WithConnectorAdapterRegistry
	WithConnectorRegistry          = gatewayoptions.WithConnectorRegistry
	WithMaintenanceConsole         = gatewayoptions.WithMaintenanceConsole
	WithRuntimeInstanceIDGenerator = gatewayoptions.WithRuntimeInstanceIDGenerator
	Health                         = gatewayroutes.Health
	RegisterRoutes                 = gatewayroutes.Register
	NewConnectorState              = gatewaystate.NewConnectorState
	NewControlState                = gatewaystate.NewControlState
	Scavenge                       = gatewayworkspace.Scavenge
	DefaultID                      = gatewayworkspace.DefaultID
	Move                           = gatewayworkspace.Move
	Publish                        = gatewayworkspace.Publish
	Adopt                          = gatewayworkspace.Adopt
	Open                           = gatewayworkspace.Open
	Discard                        = gatewayworkspace.Discard
	Close                          = gatewayworkspace.Close
	Delete                         = gatewayworkspace.Delete
	NewRegistry                    = gatewayworkspace.NewRegistry
	NewService                     = gatewayworkspace.NewService
	NewWorkspaceHTTP               = gatewayworkspace.NewHTTP
	HasActiveRemoteBackup          = gatewayworkspace.HasActiveRemoteBackup
	LooksPlaintext                 = gatewayworkspace.LooksPlaintext
	PasswordPolicyError            = gatewayworkspace.PasswordPolicyError
	UnsupportedSchemaMessage       = gatewayworkspace.UnsupportedSchemaMessage
	ValidatePassword               = gatewayworkspace.ValidatePassword
	ValidateRemoteBackupPassword   = gatewayworkspace.ValidateRemoteBackupPassword
)

const (
	AuthLockoutFailures      = gatewaystate.AuthLockoutFailures
	MCPGlobalDelayFailures   = gatewaystate.MCPGlobalDelayFailures
	MCPGlobalLockoutFailures = gatewaystate.MCPGlobalLockoutFailures
)

var (
	ErrAuthentication = gatewayworkspace.ErrAuthentication
	ErrDatabaseInUse  = gatewayworkspace.ErrDatabaseInUse
	ErrInitialization = gatewayworkspace.ErrInitialization
)

type ServerOption = gatewayoptions.Option
type RouteBackup = gatewayroutes.Backup
type RouteDependencies = gatewayroutes.Dependencies
type ConnectorState = gatewaystate.ConnectorState
type ControlState = gatewaystate.ControlState
type WorkspaceState = gatewaystate.WorkspaceState
type Runtime = workspaceruntime.Runtime
type ActionWorkflow = gatewayworkspace.ActionWorkflow
type AdoptInput = gatewayworkspace.AdoptInput
type ChangePasswordRequest = gatewayworkspace.ChangePasswordRequest
type DeleteLockedRequest = gatewayworkspace.DeleteLockedRequest
type DeleteRequest = gatewayworkspace.DeleteRequest
type WorkspaceDependencies = gatewayworkspace.Dependencies
type WorkspaceHTTPDependencies = gatewayworkspace.HTTPDependencies
type WorkspaceHTTPHandlers = gatewayworkspace.HTTPHandlers
type Identity = gatewayworkspace.Identity
type OpenInput = gatewayworkspace.OpenInput
type PasswordAttempt = gatewayworkspace.PasswordAttempt
type RenameRequest = gatewayworkspace.RenameRequest
type SetupRequest = gatewayworkspace.SetupRequest
type StatusResponse = gatewayworkspace.StatusResponse
type SwitchRequest = gatewayworkspace.SwitchRequest
type TokenStore = gatewayworkspace.TokenStore
type UnlockRequest = gatewayworkspace.UnlockRequest
type Vault = gatewayworkspace.Vault
