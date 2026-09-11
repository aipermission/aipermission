package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

var errSecurityPolicyUnavailable = errors.New("security policy runtime is unavailable")

func (s *Server) securityPolicyHTTPScope(w http.ResponseWriter) (gatewayaccess.SecurityHTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayaccess.SecurityHTTPScope{}, false
	}
	return gatewayaccess.SecurityHTTPScope{
		Service: runtime.Security.PolicyService(),
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
	}, true
}

func readSecuritySettings(ctx context.Context, runtime databaseRuntime) (gatewayaccess.SecuritySettings, error) {
	if runtime == nil || runtime.Security.PolicyService() == nil {
		return gatewayaccess.SecuritySettings{}, errSecurityPolicyUnavailable
	}
	return runtime.Security.PolicyService().ReadSettings(ctx)
}

func (s *Server) redactForPersistence(ctx context.Context, runtime databaseRuntime, value string) string {
	if runtime == nil || runtime.Security.PolicyService() == nil {
		return s.access.RedactFallback(value)
	}
	return runtime.Security.PolicyService().Redact(ctx, value)
}

func (s *Server) runtimeRedactor(runtime databaseRuntime) func(string) string {
	if runtime == nil || runtime.Security.PolicyService() == nil {
		return s.access.RedactFallback
	}
	return runtime.Security.PolicyService().Redactor()
}

func (s *Server) redactCustom(ctx context.Context, runtime databaseRuntime, value string) string {
	if runtime == nil || runtime.Security.PolicyService() == nil {
		return value
	}
	return runtime.Security.PolicyService().RedactCustom(ctx, value)
}
