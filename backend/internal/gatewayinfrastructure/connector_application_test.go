package gatewayinfrastructure

import (
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

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
