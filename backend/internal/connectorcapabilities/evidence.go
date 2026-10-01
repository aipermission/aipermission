package connectorcapabilities

import (
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

// Evidence composes only reviewed read-only capabilities. Ordinary resource and
// action providers must not be invoked by a completed-cleanup evidence check.
func Evidence(adapter connectorapi.Adapter, resources connectorapi.EvidenceResourceRuntime) (Set, error) {
	provider, ok := adapter.(connectorapi.EvidenceCapabilityProvider)
	if !ok || connectorapi.IsNilDependency(provider) || connectorapi.IsNilDependency(resources) {
		return nil, errors.New("connector cleanup evidence runtime is unavailable")
	}
	capabilities, err := Merge(nil, provider.EvidenceCapabilities(resources))
	if err != nil {
		return nil, err
	}
	for name, capability := range capabilities {
		_, network := capability.(connectors.NetworkTransport)
		_, command := capability.(connectors.CommandTransport)
		_, environment := capability.(connectors.SessionEnvironmentCapability)
		if network || command || environment || name == connectors.NetworkTransportCapabilityName ||
			name == connectors.CommandTransportCapabilityName || name == connectors.SessionEnvironmentCapabilityName {
			return nil, fmt.Errorf("cleanup evidence must not expose protected runtime capability %q", name)
		}
	}
	return capabilities, nil
}
