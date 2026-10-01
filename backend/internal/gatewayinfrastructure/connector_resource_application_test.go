package gatewayinfrastructure

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

func TestRuntimeApplicationBindsScopedProviderToActualWorkspaceAndConnector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capabilities.aipdb")
	component := NewComponent(path, nil)
	provider := &scopedCapabilityProvider{provided: map[string]connectors.RuntimeCapability{
		"domain_journal": testRuntimeCapability("domain_journal"),
	}}
	actionProvider := &actionCapabilityProvider{provided: map[string]connectors.RuntimeCapability{
		"action_service": testRuntimeCapability("action_service"),
	}}
	evidenceProvider := &evidenceCapabilityProvider{provided: map[string]connectors.RuntimeCapability{
		"local_evidence": testRuntimeCapability("local_evidence"),
	}}
	adapters := connectorapi.NewRegistry()
	for _, kind := range []string{"first", "second"} {
		if err := adapters.Register(kind, allCapabilityProviders{provider, actionProvider, evidenceProvider}); err != nil {
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
		ports: connectorports.NewPorts(connectorports.PortsDependencies{LiveConsole: connectorports.LiveConsoleDependencies{AdapterFor: adapters.For}}),
	}
	stores := []resourcecontract.CredentialResourceReader{}
	for _, mode := range []string{"normal", "approved", "credential", "evidence"} {
		before := provider.calls
		beforeAction := actionProvider.calls
		var capabilities connectors.RuntimeCapabilityResolver
		switch mode {
		case "evidence":
			capabilities, err = application.CleanupEvidenceCapabilities(handle, "first")
			if err != nil || capabilities.RuntimeCapability("local_evidence") == nil || capabilities.RuntimeCapability("domain_journal") != nil ||
				capabilities.RuntimeCapability("action_service") != nil || provider.calls != before || actionProvider.calls != beforeAction {
				t.Fatalf("evidence runtime recovered mutable/action authority: %#v %v", capabilities, err)
			}
			reader := evidenceProvider.seen.CredentialResources("domain_journal")
			if _, mutable := reader.(resourcecontract.CredentialResourceStore); mutable {
				t.Fatal("actual workspace evidence reader recovered mutable/secret store")
			}
			stores = append(stores, reader)
			continue
		case "approved":
			capabilities = application.ActionCapabilities(handle, "first", nil, nil)
		case "credential":
			capabilities = application.CredentialOperationCapabilities(handle, "first")
		default:
			capabilities = application.RuntimeCapabilities(handle, "first")
		}
		if capabilities == nil || capabilities.RuntimeCapability("domain_journal") == nil || provider.seen == nil || provider.calls != before+1 {
			t.Fatalf("actual application dropped scoped provider: mode=%s", mode)
		}
		if mode == "credential" {
			if actionProvider.calls != beforeAction || capabilities.RuntimeCapability("action_service") != nil {
				t.Fatal("credential operation exposed action runtime authority")
			}
		} else if actionProvider.calls != beforeAction+1 || capabilities.RuntimeCapability("action_service") == nil {
			t.Fatal("ordinary or approved runtime lost existing action capability composition")
		}
		stores = append(stores, provider.seen.CredentialResources("domain_journal"))
	}
	rows := []resourcecontract.CredentialResource{}
	expected := application.DataRuntime(handle, "first").CredentialResources("domain_journal")
	for index, reader := range stores {
		row, err := expected.Create(t.Context(), resourcecontract.CreateCredentialResourceInput{
			Name:         []string{"normal-entry", "approved-entry", "credential-entry", "evidence-entry"}[index],
			ResourceType: "domain.v1", PublicData: `{"status":"intent"}`, Secret: struct{}{},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := reader.Get(t.Context(), row.ID); err != nil || got != row {
			t.Fatalf("entry point bound outside actual connector resource scope: %#v %v", got, err)
		}
		rows = append(rows, row)
	}
	if application.RuntimeCapabilities(handle, "second") == nil {
		t.Fatal("second connector capability failed to compose")
	}
	for _, row := range rows {
		if _, err := expected.Get(t.Context(), row.ID); err != nil {
			t.Fatalf("entry point bound resources outside the requested connector: %v", err)
		}
		if _, err := provider.seen.CredentialResources("domain_journal").Get(t.Context(), row.ID); !errors.Is(err, resourcecontract.ErrCredentialResourceNotFound) {
			t.Fatalf("second connector read another entry point's resource: %v", err)
		}
	}
	if err := component.WorkspaceOwner().DiscardWorkspace(handle, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := provider.calls
	beforeEvidence := evidenceProvider.calls
	if application.RuntimeCapabilities(handle, "first") != nil || application.ActionCapabilities(handle, "first", nil, nil) != nil || application.CredentialOperationCapabilities(handle, "first") != nil || provider.calls != before {
		t.Fatal("retired workspace invoked a resource provider")
	}
	if result, err := application.CleanupEvidenceCapabilities(handle, "first"); err == nil || result != nil || evidenceProvider.calls != beforeEvidence {
		t.Fatal("retired workspace invoked evidence provider")
	}
	for _, store := range stores {
		if _, err := store.Get(t.Context(), rows[0].ID); err == nil {
			t.Fatal("retired workspace left a retained resource store usable")
		}
	}
}
