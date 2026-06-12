package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func (s *Server) authenticateMCP(w http.ResponseWriter, r *http.Request) (mcpAuthContext, bool) {
	limitKey := authRateLimitKey(r, "mcp")
	if err := s.authLimiter.wait(r.Context(), limitKey); err != nil {
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
		s.authLimiter.recordFailure(limitKey)
		writeError(w, http.StatusUnauthorized, "missing API token")
		return mcpAuthContext{}, false
	}

	runtimes := s.unlockedRuntimeSnapshot()
	matches := []mcpAuthContext{}
	tokenHash := tokens.HashToken(tokenValue)
	for _, runtime := range runtimes {
		var auth mcpAuthContext
		err := runtime.database.QueryRowContext(r.Context(), `
				SELECT id, name
				FROM api_tokens
				WHERE token_hash = ?
					AND COALESCE(revoked_at, '') = ''
					AND (COALESCE(expires_at, '') = '' OR expires_at > ?)`,
			tokenHash,
			time.Now().UTC().Format(time.RFC3339),
		).Scan(&auth.TokenID, &auth.Name)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			writeInternalError(w)
			return mcpAuthContext{}, false
		}
		auth.runtime = runtime
		matches = append(matches, auth)
	}
	if len(matches) > 1 {
		s.authLimiter.recordFailure(limitKey)
		writeError(w, http.StatusConflict, "API token matches multiple unlocked databases; lock or revoke duplicate token copies before using MCP")
		return mcpAuthContext{}, false
	}
	if len(matches) == 1 {
		s.authLimiter.recordSuccess(limitKey)
		return matches[0], true
	}
	if len(runtimes) == 0 {
		writeError(w, http.StatusLocked, "database is locked")
		return mcpAuthContext{}, false
	}

	s.authLimiter.recordFailure(limitKey)
	writeError(w, http.StatusUnauthorized, "invalid, revoked, or expired API token")
	return mcpAuthContext{}, false
}
