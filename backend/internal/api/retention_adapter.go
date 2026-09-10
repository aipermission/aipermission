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
	if runtime.Observation.Retention == nil {
		return retention.HTTPScope{}, true
	}
	return retention.HTTPScope{
		Service: runtime.Observation.Retention,
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
	}, true
}

func (s *Server) initializeRetention(runtime *databaseRuntime) {
	if runtime == nil || runtime.Storage.Database == nil {
		return
	}
	if runtime.Observation.Retention == nil {
		runtime.Observation.Retention = retention.NewService(runtime.Storage.Database, runtime.ID)
	}
	runtime.Observation.Retention.Start()
	s.startConnectorActionRecoveryWorker(runtime)
}

func (s *Server) stopRetention(runtime *databaseRuntime) {
	if runtime != nil && runtime.Observation.Retention != nil {
		runtime.Observation.Retention.Stop()
	}
}
