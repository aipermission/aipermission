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
		Service: runtime.SecurityPort().PolicyService(),
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
	}, true
}

func readSecuritySettings(ctx context.Context, runtime databaseRuntime) (gatewayaccess.SecuritySettings, error) {
	if runtime == nil || runtime.SecurityPort().PolicyService() == nil {
		return gatewayaccess.SecuritySettings{}, errSecurityPolicyUnavailable
	}
	return runtime.SecurityPort().PolicyService().ReadSettings(ctx)
}

func (s *Server) redactForPersistence(ctx context.Context, runtime databaseRuntime, value string) string {
	if runtime == nil || runtime.SecurityPort().PolicyService() == nil {
		return s.access.RedactBasic(value)
	}
	return runtime.SecurityPort().PolicyService().Redact(ctx, value)
}

func (s *Server) runtimeRedactor(runtime databaseRuntime) func(string) string {
	if runtime == nil || runtime.SecurityPort().PolicyService() == nil {
		return s.access.RedactBasic
	}
	return runtime.SecurityPort().PolicyService().Redactor()
}

func (s *Server) redactCustom(ctx context.Context, runtime databaseRuntime, value string) string {
	if runtime == nil || runtime.SecurityPort().PolicyService() == nil {
		return value
	}
	return runtime.SecurityPort().PolicyService().RedactCustom(ctx, value)
}
