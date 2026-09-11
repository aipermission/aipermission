package observation

import (
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
)

type State struct {
	AuditDispatcher *observability.Dispatcher
	Retention       *retention.Service
}

type Port interface {
	AuditDispatcherService() *observability.Dispatcher
	SetAuditDispatcherService(*observability.Dispatcher)
	RetentionService() *retention.Service
	SetRetentionService(*retention.Service)
}

func (s *State) AuditDispatcherService() *observability.Dispatcher {
	if s == nil {
		return nil
	}
	return s.AuditDispatcher
}

func (s *State) SetAuditDispatcherService(dispatcher *observability.Dispatcher) {
	if s != nil {
		s.AuditDispatcher = dispatcher
	}
}

func (s *State) RetentionService() *retention.Service {
	if s == nil {
		return nil
	}
	return s.Retention
}

func (s *State) SetRetentionService(service *retention.Service) {
	if s != nil {
		s.Retention = service
	}
}
