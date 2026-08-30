package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/expirypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func (s *Server) authenticateMCP(w http.ResponseWriter, r *http.Request) (mcpAuthContext, bool) {
	ipLimitKey := authRateLimitKey(r, "mcp")
	if err := s.mcpIPAuthLimiter.wait(r.Context(), ipLimitKey); err != nil {
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
		s.mcpIPAuthLimiter.recordFailure(ipLimitKey)
		writeError(w, http.StatusUnauthorized, "missing API token")
		return mcpAuthContext{}, false
	}
	tokenLimitKey := mcpTokenRateLimitKey(tokenValue)
	if err := s.mcpTokenAuthLimiter.wait(r.Context(), tokenLimitKey); err != nil {
		writeError(w, http.StatusRequestTimeout, "authentication request timed out")
		return mcpAuthContext{}, false
	}

	runtimes := s.unlockedRuntimeSnapshot()
	matches := []mcpAuthContext{}
	tokenHash := tokens.HashToken(tokenValue)
	now := time.Now().UTC()
	for _, runtime := range runtimes {
		var auth mcpAuthContext
		var expiresAt string
		err := runtime.database.QueryRowContext(r.Context(), `
			SELECT id, name, COALESCE(expires_at, '')
			FROM api_tokens
			WHERE token_hash = ?
				AND COALESCE(revoked_at, '') = ''`,
			tokenHash,
		).Scan(&auth.TokenID, &auth.Name, &expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			writeInternalError(w)
			return mcpAuthContext{}, false
		}
		if !expirypolicy.Active(expiresAt, now) {
			continue
		}
		auth.runtime = runtime
		matches = append(matches, auth)
	}
	if len(matches) > 1 {
		s.mcpIPAuthLimiter.recordSuccess(ipLimitKey)
		s.mcpTokenAuthLimiter.recordSuccess(tokenLimitKey)
		writeError(w, http.StatusConflict, "API token matches multiple unlocked databases; lock or revoke duplicate token copies before using MCP")
		return mcpAuthContext{}, false
	}
	if len(matches) == 1 {
		s.mcpIPAuthLimiter.recordSuccess(ipLimitKey)
		s.mcpTokenAuthLimiter.recordSuccess(tokenLimitKey)
		return matches[0], true
	}
	if len(runtimes) == 0 {
		writeError(w, http.StatusLocked, "database is locked")
		return mcpAuthContext{}, false
	}

	s.mcpIPAuthLimiter.recordFailure(ipLimitKey)
	s.mcpTokenAuthLimiter.recordFailure(tokenLimitKey)
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
