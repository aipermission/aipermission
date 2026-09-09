package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/retention"
)

func (s *Server) retentionHTTPScope(w http.ResponseWriter) (retention.HTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return retention.HTTPScope{}, false
	}
	if runtime.retention == nil {
		return retention.HTTPScope{}, true
	}
	return retention.HTTPScope{
		Service: runtime.retention,
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
	}, true
}

func (s *Server) initializeRetention(runtime *databaseRuntime) {
	if runtime == nil || runtime.database == nil {
		return
	}
	if runtime.retention == nil {
		runtime.retention = retention.NewService(runtime.database, runtime.id)
	}
	runtime.retention.Start()
	s.startConnectorActionRecoveryWorker(runtime)
}

func (s *Server) stopRetention(runtime *databaseRuntime) {
	if runtime != nil && runtime.retention != nil {
		runtime.retention.Stop()
	}
}
