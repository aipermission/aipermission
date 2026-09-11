// Package gatewayinfrastructure exposes process, routing, and workspace composition contracts to the API boundary.
package gatewayinfrastructure

import "github.com/aipermission/aipermission/backend/internal/gatewayworkspace"

var ErrInitialization = gatewayworkspace.ErrInitialization

type Runtime = gatewayworkspace.Runtime
type WorkspaceLifecyclePort interface{ gatewayworkspace.LifecyclePort }
type ActionWorkflow = gatewayworkspace.ActionWorkflow
type CommandWorkflow = gatewayworkspace.CommandWorkflow
type AdoptInput = gatewayworkspace.AdoptInput
type WorkspaceDependencies = gatewayworkspace.Dependencies
type WorkspaceHTTPDependencies = gatewayworkspace.HTTPDependencies
type WorkspaceHTTPHandlers = gatewayworkspace.HTTPHandlers
type Identity = gatewayworkspace.Identity
type OpenInput = gatewayworkspace.OpenInput
type PasswordAttempt = gatewayworkspace.PasswordAttempt
type TokenStore = gatewayworkspace.TokenStore
type Vault = gatewayworkspace.Vault
