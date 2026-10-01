package connectorports

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func TestCapabilitiesPreserveExplicitTransportModeWithEmptyDependencies(t *testing.T) {
	for _, mode := range []CapabilityMode{RuntimeCapabilities, ApprovedActionCapabilities} {
		for _, dependencies := range [][]connectors.ResolvedDependency{nil, {}} {
			result, err := newFactoryPorts(nil).Capabilities(Workspace{}, "fixture", mode, dependencies)
			if err != nil || result == nil {
				t.Fatalf("mode=%d composition: %v", mode, err)
			}
			network := result.RuntimeCapability(connectors.NetworkTransportCapabilityName).(networkDelegate).transport.(connectortransport.Network)
			command := result.RuntimeCapability(connectors.CommandTransportCapabilityName).(commandDelegate).transport.(connectortransport.Command)
			approved := mode == ApprovedActionCapabilities
			if (network.Approved != nil) != approved || (command.Approved != nil) != approved {
				t.Fatalf("mode=%d lost explicit approval boundary: network=%#v command=%#v", mode, network.Approved, command.Approved)
			}
		}
	}
}

func TestCapabilitiesSnapshotApprovedDependenciesPerInvocation(t *testing.T) {
	dependencies := []connectors.ResolvedDependency{{Purpose: "network", Target: connectors.TargetView{Ref: "fixture:1:2"}}}
	component := newFactoryPorts(nil)
	result, err := component.Capabilities(Workspace{}, "fixture", ApprovedActionCapabilities, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	network := result.RuntimeCapability(connectors.NetworkTransportCapabilityName).(networkDelegate).transport.(connectortransport.Network)
	command := result.RuntimeCapability(connectors.CommandTransportCapabilityName).(commandDelegate).transport.(connectortransport.Command)
	dependencies[0].Purpose = "changed"
	for _, approved := range []connectortransport.Approved{network.Approved, command.Approved} {
		if len(approved) != 1 {
			t.Fatalf("dependency snapshot lost: %#v", approved)
		}
		for _, dependency := range approved {
			if dependency.Purpose != "network" || dependency.Target.Ref != "fixture:1:2" {
				t.Fatal("approved dependency aliases the caller's slice")
			}
		}
	}
	clear(network.Approved)
	if len(command.Approved) != 1 {
		t.Fatal("network and command approval maps alias each other")
	}
	next, err := component.Capabilities(Workspace{}, "fixture", ApprovedActionCapabilities, nil)
	if err != nil || len(next.RuntimeCapability(connectors.CommandTransportCapabilityName).(commandDelegate).transport.(connectortransport.Command).Approved) != 0 {
		t.Fatalf("later invocation inherited previous dependencies: %v", err)
	}
}

func TestCapabilitiesRejectActionCollisionAfterResourceComposition(t *testing.T) {
	for _, mode := range []CapabilityMode{RuntimeCapabilities, ApprovedActionCapabilities} {
		provider := &factoryProvider{
			resourceValues: map[string]connectors.RuntimeCapability{"journal": factoryCapability("journal")},
			actionValues:   map[string]connectors.RuntimeCapability{"journal": factoryCapability("journal")},
		}
		result, err := newFactoryPorts(provider).Capabilities(Workspace{}, "fixture", mode, nil)
		if err == nil || result != nil || provider.resourceCalls != 1 || provider.actionCalls != 1 {
			t.Fatalf("action collision exposed a partial resolver: %#v %v", result, err)
		}
	}
}

func TestCapabilitiesComposeResourcesAndActionsWithoutEvidence(t *testing.T) {
	for _, mode := range []CapabilityMode{RuntimeCapabilities, ApprovedActionCapabilities} {
		provider := &factoryProvider{
			resourceValues: map[string]connectors.RuntimeCapability{"journal": factoryCapability("journal")},
			actionValues:   map[string]connectors.RuntimeCapability{"session": factoryCapability("session")},
		}
		result, err := newFactoryPorts(provider).Capabilities(Workspace{}, "fixture", mode, nil)
		if err != nil || provider.resourceCalls != 1 || provider.actionCalls != 1 || provider.evidenceCalls != 0 || provider.resources == nil {
			t.Fatalf("mode=%d provider routing: %#v %v", mode, provider, err)
		}
		delete(provider.resourceValues, "journal")
		delete(provider.actionValues, "session")
		if result.RuntimeCapability("journal") == nil || result.RuntimeCapability("session") == nil || result.RuntimeCapability("missing") != nil {
			t.Fatal("resolver aliases provider maps or invents capabilities")
		}
	}
}

func TestCapabilitiesEvidenceNeverConstructsNormalAuthority(t *testing.T) {
	provider := &factoryProvider{evidenceValues: map[string]connectors.RuntimeCapability{"journal": factoryCapability("journal")}}
	component := newFactoryPorts(provider)
	component.dependencies.Peer.TrustStorePath = func() string { panic("evidence accessed peer trust") }
	result, err := component.Capabilities(Workspace{}, "fixture", CleanupEvidenceCapabilities, nil)
	if err != nil || provider.evidenceCalls != 1 || provider.resourceCalls != 0 || provider.actionCalls != 0 {
		t.Fatalf("evidence invoked normal providers: %#v %v", provider, err)
	}
	if reflect.TypeOf(provider.evidence).NumMethod() != 1 {
		t.Fatal("evidence runtime exposes more than its read-only resource method")
	}
	if _, mutable := any(provider.evidence).(resourcecontract.ScopedResourceRuntime); mutable {
		t.Fatal("evidence adapter can recover mutable resource runtime")
	}
	for _, name := range []string{connectors.NetworkTransportCapabilityName, connectors.CommandTransportCapabilityName, connectors.SessionEnvironmentCapabilityName} {
		if result.RuntimeCapability(name) != nil {
			t.Fatalf("evidence leaked protected capability %s", name)
		}
	}
}

func TestCapabilitiesRejectInvalidResourceBeforeActionConstruction(t *testing.T) {
	for _, mode := range []CapabilityMode{RuntimeCapabilities, ApprovedActionCapabilities} {
		for name, capability := range map[string]connectors.RuntimeCapability{
			"bad-name": factoryCapability("bad-name"), "nil_value": nil, "typed_nil": (*factoryNilCapability)(nil),
			"different": factoryCapability("declared"),
			connectors.NetworkTransportCapabilityName: factoryCapability(connectors.NetworkTransportCapabilityName),
			connectors.CommandTransportCapabilityName: factoryCapability(connectors.CommandTransportCapabilityName),
		} {
			t.Run(name, func(t *testing.T) {
				provider := &factoryProvider{resourceValues: map[string]connectors.RuntimeCapability{name: capability}}
				result, err := newFactoryPorts(provider).Capabilities(Workspace{}, "fixture", mode, nil)
				if err == nil || result != nil || provider.resourceCalls != 1 || provider.actionCalls != 0 || provider.evidenceCalls != 0 {
					t.Fatalf("invalid resource exposed authority: %#v %v", provider, err)
				}
			})
		}
	}
}

func TestCapabilitiesRejectInvalidActionResultsInBothModes(t *testing.T) {
	for _, mode := range []CapabilityMode{RuntimeCapabilities, ApprovedActionCapabilities} {
		for name, capability := range map[string]connectors.RuntimeCapability{
			"bad-name": factoryCapability("bad-name"), "nil_value": nil, "typed_nil": (*factoryNilCapability)(nil),
			"different": factoryCapability("declared"),
			connectors.NetworkTransportCapabilityName: factoryCapability(connectors.NetworkTransportCapabilityName),
			connectors.CommandTransportCapabilityName: factoryCapability(connectors.CommandTransportCapabilityName),
		} {
			t.Run(name, func(t *testing.T) {
				provider := &factoryProvider{actionValues: map[string]connectors.RuntimeCapability{name: capability}}
				result, err := newFactoryPorts(provider).Capabilities(Workspace{}, "fixture", mode, nil)
				if err == nil || result != nil || provider.actionCalls != 1 || provider.evidenceCalls != 0 {
					t.Fatalf("invalid action exposed authority: %#v %v", result, err)
				}
			})
		}
	}
}

func TestCapabilitiesRejectProtectedAndInvalidEvidence(t *testing.T) {
	for name, capability := range map[string]connectors.RuntimeCapability{
		"bad-name": factoryCapability("bad-name"), "nil_value": nil, "typed_nil": (*factoryNilCapability)(nil),
		"different": factoryCapability("declared"),
		connectors.NetworkTransportCapabilityName:   factoryCapability(connectors.NetworkTransportCapabilityName),
		connectors.CommandTransportCapabilityName:   factoryCapability(connectors.CommandTransportCapabilityName),
		connectors.SessionEnvironmentCapabilityName: factoryCapability(connectors.SessionEnvironmentCapabilityName),
	} {
		t.Run(name, func(t *testing.T) {
			provider := &factoryProvider{evidenceValues: map[string]connectors.RuntimeCapability{name: capability}}
			result, err := newFactoryPorts(provider).Capabilities(Workspace{}, "fixture", CleanupEvidenceCapabilities, nil)
			if err == nil || result != nil || provider.actionCalls != 0 || provider.resourceCalls != 0 {
				t.Fatalf("invalid evidence exposed authority: %#v %v", provider, err)
			}
		})
	}
}

func TestCapabilitiesKeepInvocationWorkspaceCallbacksSeparate(t *testing.T) {
	provider := &factoryProvider{}
	component := newFactoryPorts(provider)
	for _, expected := range []error{errors.New("first invocation"), errors.New("second invocation")} {
		workspace := Workspace{Targets: WorkspaceTargetPorts{
			TargetAudit: func(context.Context, string, any) error { return expected },
		}}
		if _, err := component.Capabilities(workspace, "fixture", RuntimeCapabilities, nil); err != nil {
			t.Fatal(err)
		}
	}
	first := provider.gateways[0].(RuntimeActionGateway).workspace.Targets.TargetAudit
	second := provider.gateways[1].(RuntimeActionGateway).workspace.Targets.TargetAudit
	if first(context.Background(), "fixture", nil).Error() != "first invocation" || second(context.Background(), "fixture", nil).Error() != "second invocation" {
		t.Fatal("capability factory retained or replaced invocation-bound callbacks")
	}
}

func TestCapabilitiesFailClosedBeforeAdapterLookup(t *testing.T) {
	for _, component := range []*PortsComponent{nil, NewPorts(PortsDependencies{})} {
		if result, err := component.Capabilities(Workspace{}, "fixture", RuntimeCapabilities, nil); !errors.Is(err, ErrRuntimeUnavailable) || result != nil {
			t.Fatalf("missing factory returned authority: %#v %v", result, err)
		}
	}
	component := NewPorts(PortsDependencies{LiveConsole: LiveConsoleDependencies{
		AdapterFor: func(string) connectorapi.Adapter { panic("invalid mode consulted adapter") },
	}})
	if result, err := component.Capabilities(Workspace{}, "fixture", CapabilityMode(255), nil); err == nil || result != nil {
		t.Fatalf("invalid mode returned authority: %#v %v", result, err)
	}
	if result, err := newFactoryPorts(nil).Capabilities(Workspace{}, "fixture", CleanupEvidenceCapabilities, nil); err == nil || result != nil {
		t.Fatalf("missing evidence provider returned authority: %#v %v", result, err)
	}
}
