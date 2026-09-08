package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
)

const databasePasswordRateLimitScope = "database-password"
const authRateLimitLockoutFailures = 8

const (
	mcpGlobalDelayFailures   = 32
	mcpGlobalLockoutFailures = 64
)

type databasePasswordAttempt struct {
	limiter *runtimecontrol.Auth
	key     string
}

func (s *Server) beginDatabasePasswordAttempt(w http.ResponseWriter, r *http.Request) (databasePasswordAttempt, bool) {
	attempt := databasePasswordAttempt{
		limiter: s.authLimiter,
		key:     runtimecontrol.Key(r, databasePasswordRateLimitScope),
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
