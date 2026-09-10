package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func (s *Server) authenticateMCP(w http.ResponseWriter, r *http.Request) (mcpAuthContext, bool) {
	ipLimitKey := runtimecontrol.Key(r, "mcp")
	if err := s.mcpIPAuthLimiter.Wait(r.Context(), ipLimitKey); err != nil {
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
		s.mcpIPAuthLimiter.RecordFailure(ipLimitKey)
		writeError(w, http.StatusUnauthorized, "missing API token")
		return mcpAuthContext{}, false
	}
	tokenLimitKey := mcpTokenRateLimitKey(tokenValue)
	if err := s.mcpTokenAuthLimiter.Wait(r.Context(), tokenLimitKey); err != nil {
		writeError(w, http.StatusRequestTimeout, "authentication request timed out")
		return mcpAuthContext{}, false
	}

	runtimes := s.unlockedRuntimeSnapshot()
	matches := []mcpAuthContext{}
	tokenHash := tokens.HashToken(tokenValue)
	now := time.Now().UTC()
	for _, runtime := range runtimes {
		authenticated, err := runtime.tokens.AuthenticateHash(r.Context(), tokenHash, now)
		if errors.Is(err, tokens.ErrNotFound) {
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
		s.mcpIPAuthLimiter.RecordSuccess(ipLimitKey)
		s.mcpTokenAuthLimiter.RecordSuccess(tokenLimitKey)
		writeError(w, http.StatusConflict, "API token matches multiple unlocked databases; lock or revoke duplicate token copies before using MCP")
		return mcpAuthContext{}, false
	}
	if len(matches) == 1 {
		s.mcpIPAuthLimiter.RecordSuccess(ipLimitKey)
		s.mcpTokenAuthLimiter.RecordSuccess(tokenLimitKey)
		return matches[0], true
	}
	if len(runtimes) == 0 {
		writeError(w, http.StatusLocked, "database is locked")
		return mcpAuthContext{}, false
	}

	s.mcpIPAuthLimiter.RecordFailure(ipLimitKey)
	s.mcpTokenAuthLimiter.RecordFailure(tokenLimitKey)
	writeError(w, http.StatusUnauthorized, "invalid, revoked, or expired API token")
	return mcpAuthContext{}, false
}

func mcpTokenRateLimitKey(tokenValue string) string {
	tokenHash := strings.TrimPrefix(tokens.HashToken(tokenValue), "sha256:")
	const fingerprintLength = 24
	if len(tokenHash) > fingerprintLength {
		tokenHash = tokenHash[:fingerprintLength]
	}
	return "mcp-token:" + tokenHash
}
