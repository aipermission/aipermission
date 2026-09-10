package api

import (
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

const databasePasswordRateLimitScope = "database-password"
const authRateLimitLockoutFailures = gatewayinfra.AuthLockoutFailures

const (
	mcpGlobalDelayFailures   = gatewayinfra.MCPGlobalDelayFailures
	mcpGlobalLockoutFailures = gatewayinfra.MCPGlobalLockoutFailures
)

type databasePasswordAttempt struct {
	limiter *gatewayaccess.Auth
	key     string
}

func (s *Server) beginDatabasePasswordAttempt(w http.ResponseWriter, r *http.Request) (databasePasswordAttempt, bool) {
	attempt := databasePasswordAttempt{
		limiter: s.controlState.AuthLimiter,
		key:     gatewayaccess.RuntimeKey(r, databasePasswordRateLimitScope),
	}
	if err := attempt.limiter.Wait(r.Context(), attempt.key); err != nil {
		writeError(w, http.StatusRequestTimeout, "database password verification timed out")
		return databasePasswordAttempt{}, false
	}
	return attempt, true
}

func (attempt databasePasswordAttempt) failure() {
	attempt.limiter.RecordFailure(attempt.key)
}

func (attempt databasePasswordAttempt) success() {
	attempt.limiter.RecordSuccess(attempt.key)
}
