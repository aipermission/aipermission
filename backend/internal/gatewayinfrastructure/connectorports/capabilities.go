package connectorports

import (
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectorcapabilities"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type CapabilityMode uint8

const (
	RuntimeCapabilities CapabilityMode = iota
	ApprovedActionCapabilities
	CleanupEvidenceCapabilities
	CredentialOperationCapabilities
)

// Capabilities constructs invocation-bound ports. Evidence never constructs
// transport or action ports, even when the adapter exposes every provider.
func (component *PortsComponent) Capabilities(workspace Workspace, kind string, mode CapabilityMode, dependencies []connectors.ResolvedDependency) (connectors.RuntimeCapabilityResolver, error) {
	if component == nil || component.dependencies.LiveConsole.AdapterFor == nil {
		return nil, ErrRuntimeUnavailable
	}
	if mode != RuntimeCapabilities && mode != ApprovedActionCapabilities && mode != CleanupEvidenceCapabilities && mode != CredentialOperationCapabilities {
		return nil, fmt.Errorf("invalid connector capability mode: %d", mode)
	}
	adapterFor := component.dependencies.LiveConsole.AdapterFor
	if mode == CleanupEvidenceCapabilities {
		capabilities, err := connectorcapabilities.Evidence(adapterFor(kind), evidenceResources(ScopedResourceRuntime(workspace, kind)))
		if err != nil {
			return nil, err
		}
		return capabilities, nil
	}
	base := connectorcapabilities.Set{}
	trust := component.dependencies.Peer.TrustStorePath
	if mode == ApprovedActionCapabilities {
		base[connectors.NetworkTransportCapabilityName] = ApprovedNetworkTransport(workspace, adapterFor, trust, dependencies)
		base[connectors.CommandTransportCapabilityName] = ApprovedCommandTransport(workspace, adapterFor, trust, dependencies)
	} else {
		network := NetworkTransport(workspace, adapterFor, trust)
		base[network.ConnectorRuntimeCapability()] = network
		command := CommandTransport(workspace, adapterFor, trust)
		base[command.ConnectorRuntimeCapability()] = command
	}
	if mode == CredentialOperationCapabilities {
		return connectorcapabilities.ForResources(base, adapterFor(kind), ScopedResourceRuntime(workspace, kind))
	}
	capabilities, err := connectorcapabilities.ForRuntime(base, adapterFor(kind), ScopedResourceRuntime(workspace, kind),
		func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
			return component.RuntimeActionPorts(workspace, kind)
		})
	if err != nil {
		return nil, err
	}
	return capabilities, nil
}
