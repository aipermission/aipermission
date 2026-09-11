package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) authenticateMCP(w http.ResponseWriter, r *http.Request) (mcpAuthContext, bool) {
	ipLimitKey := gatewayaccess.RuntimeKey(r, "mcp")
	if err := s.controlState.MCPIPAuthLimiter.Wait(r.Context(), ipLimitKey); err != nil {
		writeError(w, http.StatusRequestTimeout, "authentication request timed out")
		return mcpAuthContext{}, false
	}
	tokenValue := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if tokenValue == "" {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		kind, value, ok := strings.Cut(authHeader, " ")
		if ok && strings.EqualFold(kind, "Bearer") {
			tokenValue = strings.TrimSpace(value)
		}
	}
	if tokenValue == "" {
		s.controlState.MCPIPAuthLimiter.RecordFailure(ipLimitKey)
		writeError(w, http.StatusUnauthorized, "missing API token")
		return mcpAuthContext{}, false
	}
	tokenLimitKey := mcpTokenRateLimitKey(tokenValue)
	if err := s.controlState.MCPTokenAuthLimiter.Wait(r.Context(), tokenLimitKey); err != nil {
		writeError(w, http.StatusRequestTimeout, "authentication request timed out")
		return mcpAuthContext{}, false
	}

	runtimes := s.unlockedRuntimeSnapshot()
	matches := []mcpAuthContext{}
	tokenHash := gatewayaccess.HashToken(tokenValue)
	now := time.Now().UTC()
	for _, runtime := range runtimes {
		authenticated, err := runtime.StoragePort().TokenStore().AuthenticateHash(r.Context(), tokenHash, now)
		if errors.Is(err, gatewayaccess.ErrTokenNotFound) {
			continue
		}
		if err != nil {
			writeInternalError(w)
			return mcpAuthContext{}, false
		}
		auth := mcpAuthContext{TokenID: authenticated.ID, Name: authenticated.Name, runtime: runtime}
		matches = append(matches, auth)
	}
	if len(matches) > 1 {
		s.controlState.MCPIPAuthLimiter.RecordSuccess(ipLimitKey)
		s.controlState.MCPTokenAuthLimiter.RecordSuccess(tokenLimitKey)
		writeError(w, http.StatusConflict, "API token matches multiple unlocked databases; lock or revoke duplicate token copies before using MCP")
		return mcpAuthContext{}, false
	}
	if len(matches) == 1 {
		s.controlState.MCPIPAuthLimiter.RecordSuccess(ipLimitKey)
		s.controlState.MCPTokenAuthLimiter.RecordSuccess(tokenLimitKey)
		return matches[0], true
	}
	if len(runtimes) == 0 {
		writeError(w, http.StatusLocked, "database is locked")
		return mcpAuthContext{}, false
	}

	s.controlState.MCPIPAuthLimiter.RecordFailure(ipLimitKey)
	s.controlState.MCPTokenAuthLimiter.RecordFailure(tokenLimitKey)
	writeError(w, http.StatusUnauthorized, "invalid, revoked, or expired API token")
	return mcpAuthContext{}, false
}

func mcpTokenRateLimitKey(tokenValue string) string {
	tokenHash := strings.TrimPrefix(gatewayaccess.HashToken(tokenValue), "sha256:")
	const fingerprintLength = 24
	if len(tokenHash) > fingerprintLength {
		tokenHash = tokenHash[:fingerprintLength]
	}
	return "mcp-token:" + tokenHash
}
