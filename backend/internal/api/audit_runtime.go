package api

import "github.com/aipermission/aipermission/backend/internal/auditoutbox"

func (s *Server) configureAuditDispatcher(runtime *databaseRuntime) {
	if runtime == nil || runtime.database == nil || runtime.auditDispatcher != nil {
		return
	}
	runtime.auditDispatcher = auditoutbox.NewDispatcher(runtime.database)
	runtime.auditDispatcher.Start()
}
