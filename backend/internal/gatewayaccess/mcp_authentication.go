package gatewayaccess

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

var (
	ErrMCPAuthenticationTimeout = errors.New("MCP authentication timed out")
	ErrMCPTokenMissing          = errors.New("MCP token is missing")
	ErrMCPTokenInvalid          = errors.New("MCP token is invalid, revoked, or expired")
	ErrMCPTokenDuplicate        = errors.New("MCP token matches multiple unlocked workspaces")
	ErrMCPDatabaseLocked        = errors.New("all workspaces are locked")
)

type MCPTokenSource interface {
	AuthenticateHash(context.Context, string, time.Time) (tokens.Authentication, error)
}

type MCPTokenSourceSnapshot func() []MCPTokenSource

type MCPAuthentication struct {
	TokenID     int64
	Name        string
	SourceIndex int
}

func (component *Component) AuthenticateMCP(request *http.Request, snapshot MCPTokenSourceSnapshot) (MCPAuthentication, error) {
	if component == nil || component.mcpIPLimiter == nil || component.mcpTokenLimiter == nil || request == nil || snapshot == nil {
		return MCPAuthentication{}, ErrComponentUnavailable
	}
	ipKey := runtimecontrol.Key(request, "mcp")
	if err := component.mcpIPLimiter.Wait(request.Context(), ipKey); err != nil {
		return MCPAuthentication{}, fmt.Errorf("%w: %v", ErrMCPAuthenticationTimeout, err)
	}
	tokenValue := mcpTokenValue(request)
	if tokenValue == "" {
		component.mcpIPLimiter.RecordFailure(ipKey)
		return MCPAuthentication{}, ErrMCPTokenMissing
	}
	tokenKey := mcpTokenRateLimitKey(tokenValue)
	if err := component.mcpTokenLimiter.Wait(request.Context(), tokenKey); err != nil {
		return MCPAuthentication{}, fmt.Errorf("%w: %v", ErrMCPAuthenticationTimeout, err)
	}

	sources := snapshot()
	if len(sources) == 0 {
		return MCPAuthentication{}, ErrMCPDatabaseLocked
	}
	tokenHash := tokens.HashToken(tokenValue)
	now := time.Now().UTC()
	matches := make([]MCPAuthentication, 0, 2)
	for index, source := range sources {
		if source == nil {
			return MCPAuthentication{}, fmt.Errorf("MCP token source %d is unavailable", index)
		}
		authenticated, err := source.AuthenticateHash(request.Context(), tokenHash, now)
		if errors.Is(err, tokens.ErrNotFound) {
			continue
		}
		if err != nil {
			return MCPAuthentication{}, err
		}
		matches = append(matches, MCPAuthentication{
			TokenID: authenticated.ID, Name: authenticated.Name, SourceIndex: index,
		})
	}
	if len(matches) > 1 {
		component.recordMCPSuccess(ipKey, tokenKey)
		return MCPAuthentication{}, ErrMCPTokenDuplicate
	}
	if len(matches) == 1 {
		component.recordMCPSuccess(ipKey, tokenKey)
		return matches[0], nil
	}
	component.mcpIPLimiter.RecordFailure(ipKey)
	component.mcpTokenLimiter.RecordFailure(tokenKey)
	return MCPAuthentication{}, ErrMCPTokenInvalid
}

func (component *Component) recordMCPSuccess(ipKey, tokenKey string) {
	component.mcpIPLimiter.RecordSuccess(ipKey)
	component.mcpTokenLimiter.RecordSuccess(tokenKey)
}

func mcpTokenValue(request *http.Request) string {
	tokenValue := strings.TrimSpace(request.Header.Get("X-API-Key"))
	if tokenValue != "" {
		return tokenValue
	}
	kind, value, ok := strings.Cut(strings.TrimSpace(request.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(kind, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}

func mcpTokenRateLimitKey(tokenValue string) string {
	tokenHash := strings.TrimPrefix(tokens.HashToken(tokenValue), "sha256:")
	const fingerprintLength = 24
	if len(tokenHash) > fingerprintLength {
		tokenHash = tokenHash[:fingerprintLength]
	}
	return "mcp-token:" + tokenHash
}
