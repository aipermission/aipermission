package gatewayinfrastructure

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type scopedCapabilityProvider struct {
	provided map[string]connectors.RuntimeCapability
	seen     connectorapi.ScopedResourceRuntime
	calls    int
}

func (provider *scopedCapabilityProvider) ScopedResourceCapabilities(runtime connectorapi.ScopedResourceRuntime) map[string]connectors.RuntimeCapability {
	provider.calls++
	provider.seen = runtime
	return provider.provided
}

type actionCapabilityProvider struct {
	provided map[string]connectors.RuntimeCapability
	calls    int
}

func (provider *actionCapabilityProvider) RuntimeCapabilities(connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) map[string]connectors.RuntimeCapability {
	provider.calls++
	return provider.provided
}
func (*actionCapabilityProvider) SupportsRunning(connectors.RuntimeActionContext) bool { return false }
func (*actionCapabilityProvider) FinishRunning(context.Context, connectorapi.ActionFinishGateway, connectorapi.ActionRuntime, int64, connectors.RuntimeActionContext, connectorapi.Principal, connectors.ActionHandles) error {
	return nil
}
func (*actionCapabilityProvider) RunningHint(connectorapi.ActionRequest) string { return "" }

type combinedCapabilityProvider struct {
	*scopedCapabilityProvider
	*actionCapabilityProvider
}

type capabilityActionGateway struct {
	connectorapi.RuntimeActionGateway
}
type capabilityActionRuntime struct{ connectorapi.ActionRuntime }

func TestScopedCapabilitiesDoNotRequireActionRuntimeAuthority(t *testing.T) {
	resources := connectorruntime.NewScope("fixture", connectorruntime.Dependencies{}).ScopedResourceRuntime()
	provider := &scopedCapabilityProvider{provided: map[string]connectors.RuntimeCapability{
		"domain_journal": testRuntimeCapability("domain_journal"),
	}}
	factoryCalls := 0
	result, err := composeAdapterCapabilities(runtimeCapabilities{
		connectors.NetworkTransportCapabilityName: testRuntimeCapability(connectors.NetworkTransportCapabilityName),
	}, provider, resources, func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
		factoryCalls++
		return nil, nil
	})
	if err != nil || len(result) != 2 || provider.calls != 1 || provider.seen != resources || factoryCalls != 0 {
		t.Fatalf("resource-only composition: result=%#v err=%v calls=%d factory=%d", result, err, provider.calls, factoryCalls)
	}
	delete(provider.provided, "domain_journal")
	if result["domain_journal"] == nil {
		t.Fatal("composed capabilities alias the provider map")
	}
}

func TestCapabilityCompositionPreservesActionAndCombinedProviders(t *testing.T) {
	for _, combined := range []bool{false, true} {
		action := &actionCapabilityProvider{provided: map[string]connectors.RuntimeCapability{
			"action_service": testRuntimeCapability("action_service"),
		}}
		resource := &scopedCapabilityProvider{provided: map[string]connectors.RuntimeCapability{
			"domain_journal": testRuntimeCapability("domain_journal"),
		}}
		var adapter connectorapi.Adapter = action
		if combined {
			adapter = combinedCapabilityProvider{resource, action}
		}
		factoryCalls := 0
		result, err := composeAdapterCapabilities(nil, adapter,
			connectorruntime.NewScope("fixture", connectorruntime.Dependencies{}).ScopedResourceRuntime(),
			func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
				factoryCalls++
				return capabilityActionGateway{}, capabilityActionRuntime{}
			})
		if err != nil || result["action_service"] == nil || action.calls != 1 || factoryCalls != 1 {
			t.Fatalf("action composition: combined=%v result=%#v err=%v factory=%d", combined, result, err, factoryCalls)
		}
		if combined && (result["domain_journal"] == nil || resource.calls != 1) {
			t.Fatal("combined adapter discarded scoped capabilities")
		}
	}
	base := runtimeCapabilities{"existing": testRuntimeCapability("existing")}
	if result, err := composeAdapterCapabilities(base, nil, nil, nil); err != nil || result["existing"] == nil {
		t.Fatalf("non-provider connector changed: %#v err=%v", result, err)
	}
}

func TestCapabilityCompositionFailsClosedBeforeActionAuthorityOnInvalidResources(t *testing.T) {
	resources := connectorruntime.NewScope("fixture", connectorruntime.Dependencies{}).ScopedResourceRuntime()
	for name, capability := range map[string]connectors.RuntimeCapability{
		"bad-name": testRuntimeCapability("bad-name"), "nil_capability": nil,
		"wrong_name": testRuntimeCapability("declared_name"),
		connectors.NetworkTransportCapabilityName: testRuntimeCapability(connectors.NetworkTransportCapabilityName),
	} {
		provider := combinedCapabilityProvider{
			&scopedCapabilityProvider{provided: map[string]connectors.RuntimeCapability{name: capability}},
			&actionCapabilityProvider{},
		}
		factoryCalls := 0
		result, err := composeAdapterCapabilities(runtimeCapabilities{
			connectors.NetworkTransportCapabilityName: testRuntimeCapability(connectors.NetworkTransportCapabilityName),
		}, provider, resources, func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
			factoryCalls++
			return capabilityActionGateway{}, capabilityActionRuntime{}
		})
		if err == nil || result != nil || factoryCalls != 0 || provider.actionCapabilityProvider.calls != 0 {
			t.Fatalf("invalid resource capability %q exposed authority: result=%#v err=%v factory=%d", name, result, err, factoryCalls)
		}
	}
	provider := combinedCapabilityProvider{
		&scopedCapabilityProvider{provided: map[string]connectors.RuntimeCapability{"same": testRuntimeCapability("same")}},
		&actionCapabilityProvider{provided: map[string]connectors.RuntimeCapability{"same": testRuntimeCapability("same")}},
	}
	if result, err := composeAdapterCapabilities(nil, provider, resources, func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
		return capabilityActionGateway{}, capabilityActionRuntime{}
	}); err == nil || result != nil {
		t.Fatalf("two providers silently replaced a capability: %#v err=%v", result, err)
	}
}

func TestCapabilityCompositionRejectsUnavailablePorts(t *testing.T) {
	resource := &scopedCapabilityProvider{}
	if result, err := composeAdapterCapabilities(nil, resource, nil, nil); err == nil || result != nil || resource.calls != 0 {
		t.Fatalf("missing resources were delivered: %#v err=%v calls=%d", result, err, resource.calls)
	}
	action := &actionCapabilityProvider{}
	for _, factory := range []runtimeActionPortsFactory{
		nil,
		func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
			return nil, capabilityActionRuntime{}
		},
		func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
			return capabilityActionGateway{}, nil
		},
	} {
		if result, err := composeAdapterCapabilities(nil, action, nil, factory); err == nil || result != nil || action.calls != 0 {
			t.Fatalf("missing action ports were delivered: %#v err=%v calls=%d", result, err, action.calls)
		}
	}
}
