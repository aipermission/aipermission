package api

import (
	"net/http"
)

const databasePasswordRateLimitScope = "database-password"

type databasePasswordAttempt struct {
	limiter *authRateLimiter
	key     string
}

func (s *Server) beginDatabasePasswordAttempt(w http.ResponseWriter, r *http.Request) (databasePasswordAttempt, bool) {
	attempt := databasePasswordAttempt{
		limiter: s.authLimiter,
		key:     authRateLimitKey(r, databasePasswordRateLimitScope),
	}
	if err := attempt.limiter.wait(r.Context(), attempt.key); err != nil {
		writeError(w, http.StatusRequestTimeout, "database password verification timed out")
		return databasePasswordAttempt{}, false
	}
	return attempt, true
}

func (attempt databasePasswordAttempt) failure() {
	attempt.limiter.recordFailure(attempt.key)
}

func (attempt databasePasswordAttempt) success() {
	attempt.limiter.recordSuccess(attempt.key)
}
