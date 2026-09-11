// Package gatewayinfrastructure exposes process, routing, and workspace composition contracts to the API boundary.
package gatewayinfrastructure

import (
	"github.com/aipermission/aipermission/backend/internal/gatewaystate"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
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

type Runtime = gatewayworkspace.Runtime
type WorkspaceRegistry = gatewayworkspace.Registry
type WorkspaceService = gatewayworkspace.Service
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
