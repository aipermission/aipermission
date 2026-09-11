package api

import (
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

const databasePasswordRateLimitScope = "database-password"
const authRateLimitLockoutFailures = gatewayaccess.AuthLockoutFailures

const (
	mcpGlobalDelayFailures   = gatewayaccess.MCPGlobalDelayFailures
	mcpGlobalLockoutFailures = gatewayaccess.MCPGlobalLockoutFailures
)

type databasePasswordAttempt struct {
	attempt gatewayaccess.PasswordAttempt
}

func (s *Server) beginDatabasePasswordAttempt(w http.ResponseWriter, r *http.Request) (databasePasswordAttempt, bool) {
	attempt, err := s.access.BeginPasswordAttempt(r, databasePasswordRateLimitScope)
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "database password verification timed out")
		return databasePasswordAttempt{}, false
	}
	return databasePasswordAttempt{attempt: attempt}, true
}

func (attempt databasePasswordAttempt) failure() {
	attempt.attempt.Failure()
}

func (attempt databasePasswordAttempt) success() {
	attempt.attempt.Success()
}
