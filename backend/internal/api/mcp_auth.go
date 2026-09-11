package api

import (
	"errors"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) authenticateMCP(w http.ResponseWriter, r *http.Request) (mcpAuthContext, bool) {
	var runtimes []databaseRuntime
	authentication, err := s.access.AuthenticateMCP(r, func() []gatewayaccess.MCPTokenSource {
		runtimes = s.unlockedRuntimeSnapshot()
		sources := make([]gatewayaccess.MCPTokenSource, 0, len(runtimes))
		for _, runtime := range runtimes {
			sources = append(sources, runtime.Storage.TokenStore())
		}
		return sources
	})
	if err != nil {
		writeMCPAuthenticationError(w, err)
		return mcpAuthContext{}, false
	}
	if authentication.SourceIndex < 0 || authentication.SourceIndex >= len(runtimes) {
		writeInternalError(w)
		return mcpAuthContext{}, false
	}
	return mcpAuthContext{
		TokenID: authentication.TokenID, Name: authentication.Name, runtime: runtimes[authentication.SourceIndex],
	}, true
}

func writeMCPAuthenticationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gatewayaccess.ErrMCPAuthenticationTimeout):
		writeError(w, http.StatusRequestTimeout, "authentication request timed out")
	case errors.Is(err, gatewayaccess.ErrMCPTokenMissing):
		writeError(w, http.StatusUnauthorized, "missing API token")
	case errors.Is(err, gatewayaccess.ErrMCPTokenInvalid):
		writeError(w, http.StatusUnauthorized, "invalid, revoked, or expired API token")
	case errors.Is(err, gatewayaccess.ErrMCPTokenDuplicate):
		writeError(w, http.StatusConflict, "API token matches multiple unlocked databases; lock or revoke duplicate token copies before using MCP")
	case errors.Is(err, gatewayaccess.ErrMCPDatabaseLocked):
		writeError(w, http.StatusLocked, "database is locked")
	default:
		writeInternalError(w)
	}
}
