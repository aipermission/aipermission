package gatewayaccess

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

const (
	AuthLockoutFailures      = 8
	MCPGlobalDelayFailures   = 32
	MCPGlobalLockoutFailures = 64
)

var ErrComponentUnavailable = errors.New("gateway access component is unavailable")

// Component owns process-scoped authentication throttles and browser session
// state. These controls intentionally survive workspace switches.
type Component struct {
	databasePasswordLimiter *runtimecontrol.Auth
	mcpIPLimiter            *runtimecontrol.Auth
	mcpTokenLimiter         *runtimecontrol.Auth
	vaultRevealLimiter      *runtimecontrol.Window
	vaultGenerateLimiter    *runtimecontrol.Window
	vaultRequestMu          sync.RWMutex
	vaultRequestLimiter     *runtimecontrol.Window
	uiSessions              *uisession.Manager
}

func NewComponent(frontendPort string) *Component {
	return &Component{
		databasePasswordLimiter: runtimecontrol.NewAuth(1, AuthLockoutFailures),
		mcpIPLimiter:            runtimecontrol.NewAuth(MCPGlobalDelayFailures, MCPGlobalLockoutFailures),
		mcpTokenLimiter:         runtimecontrol.NewAuth(1, AuthLockoutFailures),
		vaultRevealLimiter:      runtimecontrol.NewWindow(8, time.Minute),
		vaultGenerateLimiter:    runtimecontrol.NewWindow(10, time.Minute),
		vaultRequestLimiter:     runtimecontrol.NewWindow(30, time.Minute),
		uiSessions:              uisession.New(frontendPort),
	}
}

func (component *Component) databasePasswordFailureCount(key string) int {
	if component == nil || component.databasePasswordLimiter == nil {
		return 0
	}
	return component.databasePasswordLimiter.FailureCount(key)
}
func (component *Component) AllowVaultReveal(key string) bool {
	return component != nil && component.vaultRevealLimiter != nil && component.vaultRevealLimiter.Allow(key)
}
func (component *Component) AllowVaultGenerate(key string) bool {
	return component != nil && component.vaultGenerateLimiter != nil && component.vaultGenerateLimiter.Allow(key)
}
func (component *Component) AllowVaultRequest(key string) bool {
	if component == nil {
		return false
	}
	component.vaultRequestMu.RLock()
	limiter := component.vaultRequestLimiter
	component.vaultRequestMu.RUnlock()
	return limiter != nil && limiter.Allow(key)
}
func (component *Component) ConfigureVaultRequestLimit(limit int, window time.Duration) {
	if component != nil {
		component.vaultRequestMu.Lock()
		component.vaultRequestLimiter = runtimecontrol.NewWindow(limit, window)
		component.vaultRequestMu.Unlock()
	}
}
func (component *Component) IssueUISession(w http.ResponseWriter, databaseID, retryIdentity string) error {
	if component == nil || component.uiSessions == nil {
		return ErrComponentUnavailable
	}
	return component.uiSessions.Issue(w, databaseID, retryIdentity)
}
func (component *Component) IssuePreparedUISession(w http.ResponseWriter, prepared PreparedUISession, databaseID, retryIdentity string) error {
	if component == nil || component.uiSessions == nil {
		return ErrComponentUnavailable
	}
	return component.uiSessions.IssuePrepared(w, prepared, databaseID, retryIdentity)
}
func (component *Component) ClearUISessions(w http.ResponseWriter) {
	if component != nil && component.uiSessions != nil {
		component.uiSessions.Clear(w)
	}
}
func (component *Component) ExpireUISessionCookies(w http.ResponseWriter) {
	if component != nil && component.uiSessions != nil {
		component.uiSessions.Expire(w)
	}
}
func (component *Component) ValidUISession(r *http.Request, databaseID string) bool {
	return component != nil && component.uiSessions != nil && component.uiSessions.Valid(r, databaseID)
}
func (component *Component) InvalidateUISessions(databaseID string) {
	if component != nil && component.uiSessions != nil {
		component.uiSessions.InvalidateDatabase(databaseID)
	}
}
func (component *Component) ValidUICSRF(r *http.Request) bool {
	return component != nil && component.uiSessions != nil && component.uiSessions.ValidCSRF(r)
}
func (component *Component) EnsureUIWorkspaceCookie(w http.ResponseWriter, r *http.Request, retryIdentity string) {
	if component != nil && component.uiSessions != nil {
		component.uiSessions.EnsureWorkspaceCookie(w, r, retryIdentity)
	}
}
