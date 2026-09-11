// Package gatewayaccess exposes authorization, approval, MCP, and security contracts to the gateway.
package gatewayaccess

import (
	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

var (
	ErrBulkTargetNotFound        = commandrequests.ErrBulkTargetNotFound
	ErrCommandRuntimeUnavailable = commandrequests.ErrRuntimeUnavailable

	ErrInvalidPrincipal = executionprincipal.ErrInvalid

	ErrTokenNotFound = tokens.ErrNotFound
)

const (
	RuleAlwaysRun        = accesscontrol.RuleAlwaysRun
	VaultMetadataRead    = accesscontrol.VaultMetadataRead
	RunningAssistantHint = commandrequests.RunningAssistantHint
	CommandSourceMCP     = commandrequests.SourceMCP
	CommandSourceManual  = commandrequests.SourceManual
	CSRFCookieBase       = uisession.CSRFCookieBase
	CSRFHeaderName       = uisession.CSRFHeaderName
	SessionCookieBase    = uisession.SessionCookieBase
	SessionMaxAge        = uisession.SessionMaxAge
	WorkspaceCookieBase  = uisession.WorkspaceCookieBase
)

type AccessScope = accesscontrol.Scope
type CommandBulkAuditAppender = commandrequests.BulkAuditAppender
type CommandBulkHTTPRuntime = commandrequests.BulkHTTPRuntime
type CommandBulkTarget = commandrequests.BulkTarget
type CommandHTTPReader = commandrequests.HTTPReader
type CommandPolicyWarning = commandrequests.PolicyWarning
type CommandWorkspaceRuntimeDependencies = commandrequests.WorkspaceRuntimeDependencies
type Principal = executionprincipal.Principal
type MCPActionScope = mcpconnector.ActionScope
type MCPOutputAuthorization = mcpconnector.OutputAuthorization
type MCPPermission = mcpconnector.Permission
type MCPScope = mcpconnector.Scope
type Auth = runtimecontrol.Auth
type MCPRuntimeScope = runtimecontrol.MCPRuntimeScope
type SecurityHTTPScope = securitypolicy.HTTPScope
type SecuritySettings = securitypolicy.Settings
type Token = tokens.Token
type TokenValidationError = tokens.ValidationError
type PreparedUISession = uisession.Prepared
