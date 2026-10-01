package connectorcapabilities

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type scopedProvider struct {
	provided map[string]connectors.RuntimeCapability
	seen     resourcecontract.ScopedResourceRuntime
	calls    int
}

func (provider *scopedProvider) ScopedResourceCapabilities(resources resourcecontract.ScopedResourceRuntime) map[string]connectors.RuntimeCapability {
	provider.calls++
	provider.seen = resources
	return provider.provided
}

type actionProvider struct {
	connectorapi.RuntimeAdapter
	provided map[string]connectors.RuntimeCapability
	gateway  connectorapi.RuntimeActionGateway
	runtime  connectorapi.ActionRuntime
	calls    int
}

func (provider *actionProvider) RuntimeCapabilities(gateway connectorapi.RuntimeActionGateway, runtime connectorapi.ActionRuntime) map[string]connectors.RuntimeCapability {
	provider.calls++
	provider.gateway, provider.runtime = gateway, runtime
	return provider.provided
}

type combinedProvider struct {
	*scopedProvider
	*actionProvider
}

type scopedResources struct{}

func (*scopedResources) CredentialResources(string) resourcecontract.CredentialResourceStore {
	panic("composition must not access mutable resources")
}

type actionGateway struct {
	connectorapi.RuntimeActionGateway
}
type actionRuntime struct{ connectorapi.ActionRuntime }

func invalidCapabilities() []struct {
	name, message string
	value         connectors.RuntimeCapability
} {
	return []struct {
		name, message string
		value         connectors.RuntimeCapability
	}{
		{"bad-name", "invalid runtime capability name", capability("bad-name")},
		{"nil", "is nil", nil},
		{"typed_nil", "is nil", (*nilCapability)(nil)},
		{"different", "declares name", capability("declared")},
		{"protected", "collides", capability("protected")},
	}
}

func TestProviderCompositionSelectsOnlyRequestedAuthority(t *testing.T) {
	for _, mode := range []string{"resources", "runtime"} {
		for _, kind := range []string{"none", "resource", "action", "combined"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				resource := &scopedProvider{provided: Set{"journal": capability("journal")}}
				action := &actionProvider{provided: Set{"service": capability("service")}}
				var adapter connectorapi.Adapter
				switch kind {
				case "resource":
					adapter = resource
				case "action":
					adapter = action
				case "combined":
					adapter = combinedProvider{resource, action}
				}
				resources := &scopedResources{}
				gateway, runtime := actionGateway{}, actionRuntime{}
				factories := 0
				factory := func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
					factories++
					return gateway, runtime
				}
				base := Set{"protected": capability("protected")}
				var got Set
				var err error
				if mode == "runtime" {
					got, err = ForRuntime(base, adapter, resources, factory)
				} else {
					got, err = ForResources(base, adapter, resources)
				}
				want := Set{"protected": capability("protected")}
				wantResource, wantAction := 0, 0
				if kind == "resource" || kind == "combined" {
					wantResource = 1
					want["journal"] = capability("journal")
				}
				if mode == "runtime" && (kind == "action" || kind == "combined") {
					wantAction = 1
					want["service"] = capability("service")
				}
				if err != nil || !reflect.DeepEqual(got, want) || resource.calls != wantResource || action.calls != wantAction || factories != wantAction {
					t.Fatalf("composition=%#v err=%v dispatch=%d/%d/%d", got, err, resource.calls, factories, action.calls)
				}
				if wantResource == 1 && resource.seen != resources || wantAction == 1 && (action.gateway != gateway || action.runtime != runtime) {
					t.Fatal("provider received different authority ports")
				}
				delete(resource.provided, "journal")
				delete(action.provided, "service")
				if !reflect.DeepEqual(got, want) {
					t.Fatal("composition aliases provider maps")
				}
			})
		}
	}
}

func TestResourceFailuresStopBeforeActionFactory(t *testing.T) {
	for _, mode := range []string{"resources", "runtime"} {
		for _, invalid := range invalidCapabilities() {
			t.Run(mode+"/"+invalid.name, func(t *testing.T) {
				resource := &scopedProvider{provided: Set{invalid.name: invalid.value}}
				action := &actionProvider{}
				provider := combinedProvider{resource, action}
				base := Set{"protected": capability("protected")}
				factories := 0
				factory := func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
					factories++
					return actionGateway{}, actionRuntime{}
				}
				var got Set
				var err error
				if mode == "runtime" {
					got, err = ForRuntime(base, provider, &scopedResources{}, factory)
				} else {
					got, err = ForResources(base, provider, &scopedResources{})
				}
				if err == nil || !strings.Contains(err.Error(), invalid.message) || got != nil || resource.calls != 1 || action.calls != 0 || factories != 0 || !reflect.DeepEqual(base, Set{"protected": capability("protected")}) {
					t.Fatalf("invalid resource exposed authority: %#v %v dispatch=%d/%d/%d", got, err, resource.calls, factories, action.calls)
				}
			})
		}
	}
}

func TestProviderCompositionRejectsMissingDependencies(t *testing.T) {
	for _, mode := range []string{"resources", "runtime"} {
		for _, resources := range []resourcecontract.ScopedResourceRuntime{nil, (*scopedResources)(nil)} {
			resource, action := &scopedProvider{}, &actionProvider{}
			factories := 0
			factory := func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
				factories++
				return actionGateway{}, actionRuntime{}
			}
			var got Set
			var err error
			if mode == "runtime" {
				got, err = ForRuntime(nil, combinedProvider{resource, action}, resources, factory)
			} else {
				got, err = ForResources(nil, combinedProvider{resource, action}, resources)
			}
			if err == nil || err.Error() != "scoped connector resource runtime is unavailable" || got != nil || resource.calls != 0 || action.calls != 0 || factories != 0 {
				t.Fatalf("missing resources dispatched: mode=%s %#v %v dispatch=%d/%d/%d", mode, got, err, resource.calls, factories, action.calls)
			}
		}
	}
	for _, missing := range []string{"factory", "gateway", "runtime", "both"} {
		t.Run(missing, func(t *testing.T) {
			action := &actionProvider{}
			factories := 0
			var factory ActionPortsFactory
			message := "connector action runtime factory is unavailable"
			if missing != "factory" {
				message = "connector action runtime ports are unavailable"
				factory = func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
					factories++
					if missing == "gateway" {
						return nil, actionRuntime{}
					}
					if missing == "runtime" {
						return actionGateway{}, nil
					}
					return nil, nil
				}
			}
			got, err := ForRuntime(nil, action, nil, factory)
			wantFactories := 1
			if missing == "factory" {
				wantFactories = 0
			}
			if err == nil || err.Error() != message || got != nil || action.calls != 0 || factories != wantFactories {
				t.Fatalf("missing action dependency dispatched: %#v %v dispatch=%d/%d", got, err, factories, action.calls)
			}
		})
	}
}

func TestActionMergeRejectsInvalidCapabilitiesAndCrossProviderCollision(t *testing.T) {
	for _, invalid := range invalidCapabilities() {
		t.Run(invalid.name, func(t *testing.T) {
			resource := &scopedProvider{provided: Set{"protected": capability("protected")}}
			action := &actionProvider{provided: Set{invalid.name: invalid.value}}
			factories := 0
			got, err := ForRuntime(nil, combinedProvider{resource, action}, &scopedResources{}, func() (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
				factories++
				return actionGateway{}, actionRuntime{}
			})
			if err == nil || !strings.Contains(err.Error(), invalid.message) || got != nil || resource.calls != 1 || action.calls != 1 || factories != 1 || len(resource.provided) != 1 || len(action.provided) != 1 {
				t.Fatalf("invalid action merge=%#v %v dispatch=%d/%d/%d", got, err, resource.calls, factories, action.calls)
			}
		})
	}
}
