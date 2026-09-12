// Package gatewayaccess owns authorization, approval, MCP, and security
// contracts exposed to gateway composition.
package gatewayaccess

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
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

type Principal = executionprincipal.Principal
type SecuritySettings = securitypolicy.Settings
type TokenValidationError = tokens.ValidationError
type PreparedUISession = uisession.Prepared

type MutationRunner func(context.Context, string, func() any, func(*sql.Tx) error) error

type AccessScope struct {
	Database                *sql.DB
	Tokens                  *tokens.Store
	Registry                *connectors.Registry
	ReusableTokens          func(context.Context) (bool, error)
	Mutate                  MutationRunner
	AcquireExclusive        func(context.Context) (func(), error)
	FinishTokenInvalidation func(context.Context, int64, []int64)
}

type MCPPermission struct {
	ProjectID     int64
	ProjectName   string
	ProjectSlug   string
	TargetID      int64
	TargetName    string
	ProfileID     int64
	ProfileLabel  string
	ConnectorKind string
	ProfileKind   string
	ActionName    string
	ExecutionRule connectortargets.ActionPermissionRule
	ExpiresAt     string
}

type MCPScope struct {
	Database        *sql.DB
	Registry        *connectors.Registry
	TokenID         int64
	Permissions     func(context.Context) ([]MCPPermission, error)
	MetadataEnabled func(context.Context) (bool, error)
	Metadata        MCPMetadataResolver
}

type MCPActionCall struct {
	Source         string
	TokenID        int64
	TargetRef      string
	ActionName     string
	Input          map[string]any
	Reason         string
	IdempotencyKey string
}

type MCPActionCallResult struct {
	Request  connectortargets.ActionRequest
	Result   connectors.ActionResult
	Replayed bool
}

type MCPRunningHint func(connectortargets.ActionRequest) string

type DeliveryGate interface {
	Acquire(context.Context) (func(), error)
}

type MCPOutputAuthorization struct {
	Database   *sql.DB
	Tokens     *tokens.Store
	Leases     *vaultsessions.Store
	Delivery   DeliveryGate
	MCPStarted func() bool
	Principal  func(int64) (Principal, error)
	Now        func() time.Time
}

type MCPActionScope struct {
	Database    *sql.DB
	TokenID     int64
	Output      *MCPOutputAuthorization
	Call        func(context.Context, MCPActionCall) (MCPActionCallResult, error)
	Observe     func(context.Context, string, any)
	Redact      func(context.Context, string) string
	RunningHint MCPRunningHint
}

type MCPRuntimeState interface {
	MCPStarted() bool
	SetMCPStarted(bool)
}

type MCPRuntimeScope struct {
	State        MCPRuntimeState
	StartEnabled func(context.Context) (bool, error)
	AcquireStop  func(context.Context) (func(), error)
	StopEffects  func(context.Context) error
	Observe      func(context.Context, string, map[string]any)
	Now          func() time.Time
}

type SecurityHTTPScope struct {
	Service *securitypolicy.Service
	Mutate  MutationRunner
}

type AccessScopeProvider func(http.ResponseWriter) (AccessScope, bool)
type MCPRuntimeScopeProvider func(http.ResponseWriter) (MCPRuntimeScope, bool)
type MCPScopeProvider func(http.ResponseWriter, *http.Request) (MCPScope, bool)
type MCPActionScopeProvider func(http.ResponseWriter, *http.Request) (MCPActionScope, bool)
type SecurityHTTPScopeProvider func(http.ResponseWriter) (SecurityHTTPScope, bool)
