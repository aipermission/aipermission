package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

var errSecurityPolicyUnavailable = errors.New("security policy runtime is unavailable")

func (s *Server) securityPolicyHTTPScope(w http.ResponseWriter) (securitypolicy.HTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return securitypolicy.HTTPScope{}, false
	}
	return securitypolicy.HTTPScope{
		Service: runtime.securityPolicy,
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
	}, true
}

func readSecuritySettings(ctx context.Context, runtime *databaseRuntime) (securitypolicy.Settings, error) {
	if runtime == nil || runtime.securityPolicy == nil {
		return securitypolicy.Settings{}, errSecurityPolicyUnavailable
	}
	return runtime.securityPolicy.ReadSettings(ctx)
}

func (s *Server) redactForPersistence(ctx context.Context, runtime *databaseRuntime, value string) string {
	if runtime == nil || runtime.securityPolicy == nil {
		return securitypolicy.RedactBasic(value)
	}
	return runtime.securityPolicy.Redact(ctx, value)
}

func (s *Server) runtimeRedactor(runtime *databaseRuntime) func(string) string {
	if runtime == nil || runtime.securityPolicy == nil {
		return securitypolicy.RedactBasic
	}
	return runtime.securityPolicy.Redactor()
}

func (s *Server) redactCustom(ctx context.Context, runtime *databaseRuntime, value string) string {
	if runtime == nil || runtime.securityPolicy == nil {
		return value
	}
	return runtime.securityPolicy.RedactCustom(ctx, value)
}
