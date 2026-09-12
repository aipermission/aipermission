package gatewayinfrastructure

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

type testRuntimeCapability string

func (capability testRuntimeCapability) ConnectorRuntimeCapability() string {
	return string(capability)
}

func TestConnectorRuntimeApplicationRejectsMissingOwners(t *testing.T) {
	registry := connectorapi.NewRegistry()
	if _, err := NewConnectorRuntimeApplication(nil, nil, registry, ConnectorRuntimeDependencies{}); err == nil {
		t.Fatal("runtime application accepted a missing connector owner")
	}
	component := NewComponent(t.TempDir(), nil)
	if _, err := NewConnectorRuntimeApplication(component.ConnectorPortsOwner(), component.OperationsOwner(), nil, ConnectorRuntimeDependencies{}); err == nil {
		t.Fatal("runtime application accepted a missing adapter registry")
	}
	if _, err := NewConnectorRuntimeApplication(component.ConnectorPortsOwner(), component.OperationsOwner(), registry, ConnectorRuntimeDependencies{}); err == nil {
		t.Fatal("runtime application accepted incomplete ports")
	}
}

func TestConnectorManagementApplicationRejectsIncompleteComposition(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	runtime := &ConnectorRuntimeApplication{
		owner: component.ConnectorPortsOwner(), adapters: connectorapi.NewRegistry(),
		ports: connectorports.NewPorts(connectorports.PortsDependencies{}),
	}
	if _, err := NewConnectorManagementApplication(nil, runtime, ConnectorManagementPorts{}); err == nil {
		t.Fatal("management application accepted a missing owner")
	}
	if _, err := NewConnectorManagementApplication(component.ConnectorManagementOwner(), runtime, ConnectorManagementPorts{}); err == nil {
		t.Fatal("management application accepted incomplete ports")
	}
}

func TestMergeRuntimeCapabilitiesRejectsProtectedCapabilityCollision(t *testing.T) {
	protected := testRuntimeCapability(connectors.NetworkTransportCapabilityName)
	base := runtimeCapabilities{connectors.NetworkTransportCapabilityName: protected}
	merged, err := mergeRuntimeCapabilities(base, map[string]connectors.RuntimeCapability{
		connectors.NetworkTransportCapabilityName: protected,
	})
	if err == nil || merged != nil {
		t.Fatal("adapter capability replaced a protected transport capability")
	}
	if base[connectors.NetworkTransportCapabilityName] != protected {
		t.Fatal("failed merge mutated the protected capability set")
	}
}

func TestMergeRuntimeCapabilitiesValidatesAndSnapshotsAdapterCapabilities(t *testing.T) {
	provided := map[string]connectors.RuntimeCapability{
		"session_environment": testRuntimeCapability("session_environment"),
	}
	merged, err := mergeRuntimeCapabilities(runtimeCapabilities{
		connectors.CommandTransportCapabilityName: testRuntimeCapability(connectors.CommandTransportCapabilityName),
	}, provided)
	if err != nil {
		t.Fatal(err)
	}
	delete(provided, "session_environment")
	if merged["session_environment"] == nil {
		t.Fatal("merged capability set aliases the adapter map")
	}

	for name, capability := range map[string]connectors.RuntimeCapability{
		"":          testRuntimeCapability("missing_name"),
		"bad-name":  testRuntimeCapability("bad-name"),
		"nil_value": nil,
		"map_name":  testRuntimeCapability("declared_name"),
	} {
		if result, err := mergeRuntimeCapabilities(nil, map[string]connectors.RuntimeCapability{name: capability}); err == nil || result != nil {
			t.Fatalf("invalid capability %q was accepted", name)
		}
	}
}
