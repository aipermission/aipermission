// Package connectorcapabilities composes reviewed connector capability sets.
package connectorcapabilities

import (
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type Set map[string]connectors.RuntimeCapability

func (capabilities Set) RuntimeCapability(name string) connectors.RuntimeCapability {
	return capabilities[name]
}

// Merge never replaces a core capability or retains either mutable input map.
func Merge(base Set, additions map[string]connectors.RuntimeCapability) (Set, error) {
	merged := make(Set, len(base)+len(additions))
	for name, capability := range base {
		merged[name] = capability
	}
	for name, capability := range additions {
		if !connectors.ValidIdentifier(name) {
			return nil, fmt.Errorf("invalid runtime capability name %q", name)
		}
		if connectorapi.IsNilDependency(capability) {
			return nil, fmt.Errorf("runtime capability %q is nil", name)
		}
		if declared := capability.ConnectorRuntimeCapability(); declared != name {
			return nil, fmt.Errorf("runtime capability %q declares name %q", name, declared)
		}
		if _, exists := merged[name]; exists {
			return nil, fmt.Errorf("runtime capability %q collides with a protected capability", name)
		}
		merged[name] = capability
	}
	return merged, nil
}
