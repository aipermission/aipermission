package api

import "github.com/aipermission/aipermission/backend/internal/observability"

func (s *Server) configureAuditDispatcher(runtime *databaseRuntime) {
	if runtime == nil || runtime.Storage.Database == nil || runtime.Observation.AuditDispatcher != nil {
		return
	}
	runtime.Observation.AuditDispatcher = observability.NewDispatcher(runtime.Storage.Database)
	runtime.Observation.AuditDispatcher.Start()
}
