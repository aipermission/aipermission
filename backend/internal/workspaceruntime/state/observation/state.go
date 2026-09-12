package observation

import (
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
)

type State struct {
	auditDispatcher *observability.Dispatcher
	retention       *retention.Service
}

func (s *State) AuditDispatcherService() *observability.Dispatcher {
	if s == nil {
		return nil
	}
	return s.auditDispatcher
}

func (s *State) SetAuditDispatcherService(dispatcher *observability.Dispatcher) {
	if s != nil {
		s.auditDispatcher = dispatcher
	}
}

func (s *State) RetentionService() *retention.Service {
	if s == nil {
		return nil
	}
	return s.retention
}

func (s *State) SetRetentionService(service *retention.Service) {
	if s != nil {
		s.retention = service
	}
}
