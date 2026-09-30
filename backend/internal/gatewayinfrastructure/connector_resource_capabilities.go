package gatewayinfrastructure

import (
	"errors"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type runtimeActionPortsFactory func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime)

func composeAdapterCapabilities(
	base runtimeCapabilities, adapter connectorapi.Adapter,
	resources connectorapi.ScopedResourceRuntime, actionPorts runtimeActionPortsFactory,
) (runtimeCapabilities, error) {
	capabilities := base
	var err error
	if provider, ok := adapter.(connectorapi.ScopedResourceCapabilityProvider); ok {
		if resources == nil {
			return nil, errors.New("scoped connector resource runtime is unavailable")
		}
		capabilities, err = mergeRuntimeCapabilities(capabilities, provider.ScopedResourceCapabilities(resources))
		if err != nil {
			return nil, err
		}
	}
	if provider, ok := adapter.(connectorapi.RuntimeAdapter); ok {
		if actionPorts == nil {
			return nil, errors.New("connector action runtime factory is unavailable")
		}
		gateway, runtime := actionPorts()
		if gateway == nil || runtime == nil {
			return nil, errors.New("connector action runtime ports are unavailable")
		}
		capabilities, err = mergeRuntimeCapabilities(capabilities, provider.RuntimeCapabilities(gateway, runtime))
		if err != nil {
			return nil, err
		}
	}
	return capabilities, nil
}
