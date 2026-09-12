package api

import (
	"errors"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) authenticateMCP(w http.ResponseWriter, r *http.Request) (mcpAuthContext, bool) {
	var candidates []*gatewayinfra.WorkspaceHandle
	authentication, err := s.access.AuthenticateMCP(r, func() []gatewayaccess.MCPTokenSource {
		sources, eligible := mcpAuthenticationSources(s.unlockedRuntimeSnapshot(), s.infrastructure.MCPTokenSource)
		candidates = eligible
		return sources
	})
	if err != nil {
		writeMCPAuthenticationError(w, err)
		return mcpAuthContext{}, false
	}
	if authentication.SourceIndex < 0 || authentication.SourceIndex >= len(candidates) {
		writeInternalError(w)
		return mcpAuthContext{}, false
	}
	return mcpAuthContext{
		TokenID: authentication.TokenID, Name: authentication.Name, runtime: candidates[authentication.SourceIndex],
	}, true
}

func mcpAuthenticationSources(
	runtimes []*gatewayinfra.WorkspaceHandle,
	resolve func(*gatewayinfra.WorkspaceHandle) (gatewayaccess.MCPTokenSource, bool),
) ([]gatewayaccess.MCPTokenSource, []*gatewayinfra.WorkspaceHandle) {
	if resolve == nil {
		return nil, nil
	}
	sources := make([]gatewayaccess.MCPTokenSource, 0, len(runtimes))
	eligible := make([]*gatewayinfra.WorkspaceHandle, 0, len(runtimes))
	for _, runtime := range runtimes {
		if source, ok := resolve(runtime); ok && source != nil {
			sources = append(sources, source)
			eligible = append(eligible, runtime)
		}
	}
	return sources, eligible
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
