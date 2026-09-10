// Package gatewayvault exposes project, secret, and Vault-session application contracts to the gateway.
package gatewayvault

import (
	"github.com/aipermission/aipermission/backend/internal/applicationvault"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

var (
	NewApplication                   = applicationvault.New
	NewProjectsHTTPHandlers          = projects.NewHTTPHandlers
	WriteProjectHTTPError            = projects.WriteHTTPError
	EnsureWorkspaceUUID              = projectvault.EnsureWorkspaceUUID
	NewProjectVaultHTTPHandlers      = projectvault.NewHTTPHandlers
	DecryptJSON                      = recordcrypto.DecryptJSON
	IsUIExempt                       = uisession.IsExempt
	PrepareUISession                 = uisession.Prepare
	UISessionRetryIdentity           = uisession.RetryIdentity
	NewVaultApprovalHTTPHandlers     = vaultrequests.NewHTTPHandlers
	NewVaultMCPHTTPHandlers          = vaultrequests.NewMCPHTTPHandlers
	ErrInvalidatorUnavailable        = vaultsessions.ErrInvalidatorUnavailable
	NewInvalidator                   = vaultsessions.NewInvalidator
	NewPersistence                   = vaultsessions.NewPersistence
	ConnectorCredentialProfileRecord = recordcrypto.ConnectorCredentialProfile
)

const (
	CSRFCookieBase      = uisession.CSRFCookieBase
	CSRFHeaderName      = uisession.CSRFHeaderName
	SessionCookieBase   = uisession.SessionCookieBase
	SessionMaxAge       = uisession.SessionMaxAge
	WorkspaceCookieBase = uisession.WorkspaceCookieBase
	ActionGenerateItem  = vaultrequests.ActionGenerateItem
)

type Application = applicationvault.Component
type ActionDependencies = applicationvault.ActionDependencies
type ProjectDependencies = applicationvault.ProjectDependencies
type RequestDependencies = applicationvault.RequestDependencies
type ProjectScope = projects.Scope
type ProjectVaultHTTPScope = projectvault.HTTPScope
type ProjectVaultRuntime = projectvault.Runtime
type SessionMutationScope = projectvault.SessionMutationScope
type SessionReference = projectvault.SessionReference
type SessionSelection = projectvault.SessionSelection
type PreparedUISession = uisession.Prepared
type VaultConnectorPort = vaultactions.ConnectorPort
type PeerIdentityExpectation = vaultactions.PeerIdentityExpectation
type VaultActionRuntime = vaultactions.Runtime
type VaultApprovalHTTPScope = vaultrequests.HTTPScope
type VaultMCPHTTPScope = vaultrequests.MCPHTTPScope
type VaultRequestRuntime = vaultrequests.Runtime
type Invalidator = vaultsessions.Invalidator
type InvalidatorDependencies = vaultsessions.InvalidatorDependencies
type VaultSessionReference = vaultsessions.Reference
type RequestInvalidator = vaultsessions.RequestInvalidator
