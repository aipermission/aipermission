package gatewayworkspace

import observationcapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/observation"

func (runtime *Runtime) ObservationCapability() (observationcapability.Capability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return observationcapability.Capability{}, false
	}
	policy := owner.Security.PolicyService()
	return observationcapability.New(
		owner.Storage.DatabaseHandle(), owner.Connectors.ConnectorRegistry(), owner.Security.RuntimeControlState().MCPStarted,
		policy,
		owner.Observation.AuditDispatcherService, owner.Observation.SetAuditDispatcherService,
		owner.Observation.RetentionService, owner.Observation.SetRetentionService,
	), true
}
