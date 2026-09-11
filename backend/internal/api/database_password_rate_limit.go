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
	access *gatewayaccess.Component
	key    string
}

func (s *Server) beginDatabasePasswordAttempt(w http.ResponseWriter, r *http.Request) (databasePasswordAttempt, bool) {
	attempt := databasePasswordAttempt{
		access: s.access,
		key:    gatewayaccess.RuntimeKey(r, databasePasswordRateLimitScope),
	}
	if err := attempt.access.WaitDatabasePassword(r.Context(), attempt.key); err != nil {
		writeError(w, http.StatusRequestTimeout, "database password verification timed out")
		return databasePasswordAttempt{}, false
	}
	return attempt, true
}

func (attempt databasePasswordAttempt) failure() {
	attempt.access.RecordDatabasePasswordFailure(attempt.key)
}

func (attempt databasePasswordAttempt) success() {
	attempt.access.RecordDatabasePasswordSuccess(attempt.key)
}
