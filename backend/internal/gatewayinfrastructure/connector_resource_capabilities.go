package gatewayinfrastructure

import (
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectorcapabilities"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type runtimeActionPortsFactory func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime)

func composeAdapterCapabilities(
	base runtimeCapabilities, adapter connectorapi.Adapter,
	resources connectorapi.ScopedResourceRuntime, actionPorts runtimeActionPortsFactory,
) (runtimeCapabilities, error) {
	capabilities, err := composeScopedResourceCapabilities(base, adapter, resources)
	if err != nil {
		return nil, err
	}
	if provider, ok := adapter.(connectorapi.RuntimeAdapter); ok {
		if actionPorts == nil {
			return nil, errors.New("connector action runtime factory is unavailable")
		}
		gateway, runtime := actionPorts()
		if gateway == nil || runtime == nil {
			return nil, errors.New("connector action runtime ports are unavailable")
		}
		capabilities, err = connectorcapabilities.Merge(capabilities, provider.RuntimeCapabilities(gateway, runtime))
		if err != nil {
			return nil, err
		}
	}
	return capabilities, nil
}

// Credential operator operations must never construct action/session ports,
// even when the same adapter implements both capability provider contracts.
func composeScopedResourceCapabilities(base runtimeCapabilities, adapter connectorapi.Adapter, resources connectorapi.ScopedResourceRuntime) (runtimeCapabilities, error) {
	if provider, ok := adapter.(connectorapi.ScopedResourceCapabilityProvider); ok {
		if connectorapi.IsNilDependency(resources) {
			return nil, errors.New("scoped connector resource runtime is unavailable")
		}
		return connectorcapabilities.Merge(base, provider.ScopedResourceCapabilities(resources))
	}
	return base, nil
}
