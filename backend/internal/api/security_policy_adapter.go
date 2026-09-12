package api

import (
	"context"
	"errors"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

var errSecurityPolicyUnavailable = errors.New("security policy runtime is unavailable")

func (s *Server) readSecuritySettings(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) (gatewayaccess.SecuritySettings, error) {
	if s == nil || runtime == nil {
		return gatewayaccess.SecuritySettings{}, errSecurityPolicyUnavailable
	}
	settings, err := s.accessOwner.ReadSecuritySettings(ctx, runtime)
	if err != nil {
		return gatewayaccess.SecuritySettings{}, errSecurityPolicyUnavailable
	}
	return settings, nil
}

func (s *Server) redactForPersistence(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, value string) string {
	if runtime == nil {
		return s.access.RedactFallback(value)
	}
	if redacted, ok := s.accessOwner.RedactForPersistence(ctx, runtime, value); ok {
		return redacted
	}
	return s.access.RedactFallback(value)
}

func (s *Server) runtimeRedactor(runtime *gatewayinfra.WorkspaceHandle) func(string) string {
	if runtime == nil {
		return s.access.RedactFallback
	}
	if redact, ok := s.accessOwner.RuntimeRedactor(runtime); ok {
		return redact
	}
	return s.access.RedactFallback
}

func (s *Server) redactCustom(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, value string) string {
	if runtime == nil {
		return value
	}
	if redacted, ok := s.accessOwner.RedactCustom(ctx, runtime, value); ok {
		return redacted
	}
	return value
}
