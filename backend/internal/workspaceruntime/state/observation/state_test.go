package observation

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
)

func TestStateOwnsObservationServices(t *testing.T) {
	var state State
	if state.AuditDispatcherService() != nil || state.RetentionService() != nil {
		t.Fatal("zero observation state is not empty")
	}
	dispatcher := &observability.Dispatcher{}
	service := &retention.Service{}
	state.SetAuditDispatcherService(dispatcher)
	state.SetRetentionService(service)
	if state.AuditDispatcherService() != dispatcher || state.RetentionService() != service {
		t.Fatal("observation services were not retained")
	}
	var nilState *State
	nilState.SetAuditDispatcherService(dispatcher)
	nilState.SetRetentionService(service)
}
