package connectorports

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type factoryCapability string

func (value factoryCapability) ConnectorRuntimeCapability() string { return string(value) }

type factoryNilCapability struct{}

func (*factoryNilCapability) ConnectorRuntimeCapability() string { panic("typed nil invoked") }

type factoryProvider struct {
	resourceCalls, actionCalls, evidenceCalls    int
	resourceValues, actionValues, evidenceValues map[string]connectors.RuntimeCapability
	resources                                    resourcecontract.ScopedResourceRuntime
	evidence                                     resourcecontract.EvidenceResourceRuntime
	gateways                                     []connectorapi.RuntimeActionGateway
}

func (provider *factoryProvider) ScopedResourceCapabilities(runtime resourcecontract.ScopedResourceRuntime) map[string]connectors.RuntimeCapability {
	provider.resourceCalls++
	provider.resources = runtime
	return provider.resourceValues
}

func (provider *factoryProvider) RuntimeCapabilities(gateway connectorapi.RuntimeActionGateway, _ connectorapi.ActionRuntime) map[string]connectors.RuntimeCapability {
	provider.actionCalls++
	provider.gateways = append(provider.gateways, gateway)
	return provider.actionValues
}

func (*factoryProvider) SupportsRunning(connectors.RuntimeActionContext) bool { return false }
func (*factoryProvider) FinishRunning(context.Context, connectorapi.ActionFinishGateway, connectorapi.ActionRuntime, int64, connectors.RuntimeActionContext, connectorapi.Principal, connectors.ActionHandles) error {
	return nil
}
func (*factoryProvider) RunningHint(connectorapi.ActionRequest) string { return "" }

func (provider *factoryProvider) EvidenceCapabilities(runtime resourcecontract.EvidenceResourceRuntime) map[string]connectors.RuntimeCapability {
	provider.evidenceCalls++
	provider.evidence = runtime
	return provider.evidenceValues
}

func newFactoryPorts(adapter connectorapi.Adapter) *PortsComponent {
	return NewPorts(PortsDependencies{LiveConsole: LiveConsoleDependencies{
		AdapterFor: func(string) connectorapi.Adapter { return adapter },
	}})
}
