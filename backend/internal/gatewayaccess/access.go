// Package gatewayaccess exposes authorization, approval, MCP, and security contracts to the gateway.
package gatewayaccess

import (
	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

func InvalidPrincipalError() error { return executionprincipal.ErrInvalid }
func TokenNotFoundError() error    { return tokens.ErrNotFound }

const (
	CSRFCookieBase      = uisession.CSRFCookieBase
	CSRFHeaderName      = uisession.CSRFHeaderName
	SessionCookieBase   = uisession.SessionCookieBase
	SessionMaxAge       = uisession.SessionMaxAge
	WorkspaceCookieBase = uisession.WorkspaceCookieBase
)

type AccessScope = accesscontrol.Scope
type Principal = executionprincipal.Principal
type MCPActionScope = mcpconnector.ActionScope
type MCPActionCall = mcpconnector.ActionCall
type MCPActionCallResult = mcpconnector.ActionCallResult
type MCPOutputAuthorization = mcpconnector.OutputAuthorization
type MCPPermission = mcpconnector.Permission
type MCPScope = mcpconnector.Scope
type MCPRuntimeScope = runtimecontrol.MCPRuntimeScope
type SecurityHTTPScope = securitypolicy.HTTPScope
type SecuritySettings = securitypolicy.Settings
type Token = tokens.Token
type TokenValidationError = tokens.ValidationError
type PreparedUISession = uisession.Prepared
