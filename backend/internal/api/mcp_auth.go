package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) authenticateMCP(w http.ResponseWriter, r *http.Request) (mcpAuthContext, bool) {
	ipLimitKey := s.access.RuntimeKey(r, "mcp")
	if err := s.access.WaitMCPIP(r.Context(), ipLimitKey); err != nil {
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
		s.access.RecordMCPIPFailure(ipLimitKey)
		writeError(w, http.StatusUnauthorized, "missing API token")
		return mcpAuthContext{}, false
	}
	tokenLimitKey := s.mcpTokenRateLimitKey(tokenValue)
	if err := s.access.WaitMCPToken(r.Context(), tokenLimitKey); err != nil {
		writeError(w, http.StatusRequestTimeout, "authentication request timed out")
		return mcpAuthContext{}, false
	}

	runtimes := s.unlockedRuntimeSnapshot()
	matches := []mcpAuthContext{}
	tokenHash := s.access.HashToken(tokenValue)
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
		s.access.RecordMCPIPSuccess(ipLimitKey)
		s.access.RecordMCPTokenSuccess(tokenLimitKey)
		writeError(w, http.StatusConflict, "API token matches multiple unlocked databases; lock or revoke duplicate token copies before using MCP")
		return mcpAuthContext{}, false
	}
	if len(matches) == 1 {
		s.access.RecordMCPIPSuccess(ipLimitKey)
		s.access.RecordMCPTokenSuccess(tokenLimitKey)
		return matches[0], true
	}
	if len(runtimes) == 0 {
		writeError(w, http.StatusLocked, "database is locked")
		return mcpAuthContext{}, false
	}

	s.access.RecordMCPIPFailure(ipLimitKey)
	s.access.RecordMCPTokenFailure(tokenLimitKey)
	writeError(w, http.StatusUnauthorized, "invalid, revoked, or expired API token")
	return mcpAuthContext{}, false
}

func (s *Server) mcpTokenRateLimitKey(tokenValue string) string {
	tokenHash := strings.TrimPrefix(s.access.HashToken(tokenValue), "sha256:")
	const fingerprintLength = 24
	if len(tokenHash) > fingerprintLength {
		tokenHash = tokenHash[:fingerprintLength]
	}
	return "mcp-token:" + tokenHash
}
