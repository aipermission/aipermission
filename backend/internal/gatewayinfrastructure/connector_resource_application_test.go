package gatewayinfrastructure

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

func TestRuntimeApplicationBindsScopedProviderToActualWorkspaceAndConnector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capabilities.aipdb")
	component := NewComponent(path, nil)
	provider := &scopedCapabilityProvider{provided: map[string]connectors.RuntimeCapability{
		"domain_journal": testRuntimeCapability("domain_journal"),
	}}
	adapters := connectorapi.NewRegistry()
	for _, kind := range []string{"first", "second"} {
		if err := adapters.Register(kind, provider); err != nil {
			t.Fatal(err)
		}
	}
	handle, err := component.WorkspaceOwner().OpenWorkspace(t.Context(), NewOpenWorkspaceInput(
		"fixture", path, "ScopedCapabilityPassword123", "scope-fixture-only", connectors.NewRegistry(), adapters,
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if component.WorkspaceOwner().valid(handle) {
			if err := component.WorkspaceOwner().DiscardWorkspace(handle, nil, nil); err != nil {
				t.Errorf("discard fixture workspace: %v", err)
			}
		}
	})
	application := &ConnectorRuntimeApplication{
		owner: component.ConnectorPortsOwner(), adapters: adapters,
		ports: connectorports.NewPorts(connectorports.PortsDependencies{}),
	}
	stores := []connectorapi.CredentialResourceStore{}
	for _, approved := range []bool{false, true} {
		before := provider.calls
		var capabilities connectors.RuntimeCapabilityResolver
		if approved {
			capabilities = application.ActionCapabilities(handle, "first", nil, nil)
		} else {
			capabilities = application.RuntimeCapabilities(handle, "first")
		}
		if capabilities == nil || capabilities.RuntimeCapability("domain_journal") == nil || provider.seen == nil || provider.calls != before+1 {
			t.Fatalf("actual application dropped scoped provider: approved=%v", approved)
		}
		stores = append(stores, provider.seen.CredentialResources("domain_journal"))
	}
	rows := []connectorapi.CredentialResource{}
	for index, store := range stores {
		row, err := store.Create(t.Context(), connectorapi.CreateCredentialResourceInput{
			Name:         []string{"normal-entry", "approved-entry"}[index],
			ResourceType: "domain.v1", PublicData: `{"status":"intent"}`, Secret: struct{}{},
		})
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	expected := application.DataRuntime(handle, "first").CredentialResources("domain_journal")
	if application.RuntimeCapabilities(handle, "second") == nil {
		t.Fatal("second connector capability failed to compose")
	}
	for _, row := range rows {
		if _, err := expected.Get(t.Context(), row.ID); err != nil {
			t.Fatalf("entry point bound resources outside the requested connector: %v", err)
		}
		if _, err := provider.seen.CredentialResources("domain_journal").Get(t.Context(), row.ID); !errors.Is(err, connectorapi.ErrCredentialResourceNotFound) {
			t.Fatalf("second connector read another entry point's resource: %v", err)
		}
	}
	if err := component.WorkspaceOwner().DiscardWorkspace(handle, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := provider.calls
	if application.RuntimeCapabilities(handle, "first") != nil || application.ActionCapabilities(handle, "first", nil, nil) != nil || provider.calls != before {
		t.Fatal("retired workspace invoked a resource provider")
	}
	for _, store := range stores {
		if _, err := store.Get(t.Context(), rows[0].ID); err == nil {
			t.Fatal("retired workspace left a retained resource store usable")
		}
	}
}
