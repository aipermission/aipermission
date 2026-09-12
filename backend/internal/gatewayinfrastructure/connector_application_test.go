package gatewayinfrastructure

import (
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestConnectorRuntimeApplicationRejectsMissingOwners(t *testing.T) {
	registry := connectorapi.NewRegistry()
	if _, err := NewConnectorRuntimeApplication(nil, registry, ConnectorRuntimeDependencies{}); err == nil {
		t.Fatal("runtime application accepted a missing connector owner")
	}
	component := NewComponent(t.TempDir(), nil)
	if _, err := NewConnectorRuntimeApplication(component.ConnectorPortsOwner(), nil, ConnectorRuntimeDependencies{}); err == nil {
		t.Fatal("runtime application accepted a missing adapter registry")
	}
}

func TestConnectorManagementApplicationRejectsIncompleteComposition(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	runtime, err := NewConnectorRuntimeApplication(
		component.ConnectorPortsOwner(), connectorapi.NewRegistry(), ConnectorRuntimeDependencies{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewConnectorManagementApplication(nil, runtime, ConnectorManagementPorts{}); err == nil {
		t.Fatal("management application accepted a missing owner")
	}
	if _, err := NewConnectorManagementApplication(component.ConnectorManagementOwner(), runtime, ConnectorManagementPorts{}); err == nil {
		t.Fatal("management application accepted incomplete ports")
	}
}
