package connectorcapabilities

import (
	"errors"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type ActionPortsFactory func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime)

func ForRuntime(base Set, adapter connectorapi.Adapter, resources resourcecontract.ScopedResourceRuntime, actionPorts ActionPortsFactory) (Set, error) {
	capabilities, err := ForResources(base, adapter, resources)
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
		capabilities, err = Merge(capabilities, provider.RuntimeCapabilities(gateway, runtime))
		if err != nil {
			return nil, err
		}
	}
	return capabilities, nil
}

// ForResources cannot construct action/session ports, even for an adapter that
// implements both provider contracts. Callers separately supply protected ports.
func ForResources(base Set, adapter connectorapi.Adapter, resources resourcecontract.ScopedResourceRuntime) (Set, error) {
	if provider, ok := adapter.(connectorapi.ScopedResourceCapabilityProvider); ok {
		if resourcecontract.IsNilDependency(resources) {
			return nil, errors.New("scoped connector resource runtime is unavailable")
		}
		return Merge(base, provider.ScopedResourceCapabilities(resources))
	}
	return base, nil
}
