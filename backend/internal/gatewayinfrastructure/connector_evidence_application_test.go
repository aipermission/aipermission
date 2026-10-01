package gatewayinfrastructure

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

type evidenceApplicationFixture struct {
	component   *Component
	handle      *WorkspaceHandle
	application *ConnectorRuntimeApplication
	provider    allCapabilityProviders
}

func newEvidenceApplicationFixture(t *testing.T, name string) evidenceApplicationFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".aipdb")
	component := NewComponent(path, nil)
	provider := allCapabilityProviders{
		&scopedCapabilityProvider{}, &actionCapabilityProvider{},
		&evidenceCapabilityProvider{provided: map[string]connectors.RuntimeCapability{"journal": testRuntimeCapability("journal")}},
	}
	adapters := connectorapi.NewRegistry()
	for _, kind := range []string{"first", "second"} {
		if err := adapters.Register(kind, provider); err != nil {
			t.Fatal(err)
		}
	}
	handle, err := component.WorkspaceOwner().OpenWorkspace(t.Context(), NewOpenWorkspaceInput(
		name, path, "EvidenceFixturePassword123", "evidence-fixture-only", connectors.NewRegistry(), adapters,
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if component.WorkspaceOwner().valid(handle) {
			if err := component.WorkspaceOwner().DiscardWorkspace(handle, nil, nil); err != nil {
				t.Errorf("discard evidence workspace: %v", err)
			}
		}
	})
	application := &ConnectorRuntimeApplication{
		owner: component.ConnectorPortsOwner(), adapters: adapters,
		ports: connectorports.NewPorts(connectorports.PortsDependencies{LiveConsole: connectorports.LiveConsoleDependencies{AdapterFor: adapters.For}}),
	}
	return evidenceApplicationFixture{component, handle, application, provider}
}

func (fixture evidenceApplicationFixture) reader(t *testing.T, kind string) connectorapi.CredentialResourceReader {
	t.Helper()
	if result, err := fixture.application.CleanupEvidenceCapabilities(fixture.handle, kind); err != nil || result == nil || result.RuntimeCapability("journal") == nil {
		t.Fatalf("evidence entry point failed: %#v %v", result, err)
	}
	if fixture.provider.scopedCapabilityProvider.calls != 0 || fixture.provider.actionCapabilityProvider.calls != 0 {
		t.Fatal("evidence entry point invoked mutable or action providers")
	}
	reader := fixture.provider.evidenceCapabilityProvider.seen.CredentialResources("journal")
	if _, mutable := reader.(connectorapi.CredentialResourceStore); mutable {
		t.Fatal("evidence exposed mutable store")
	}
	return reader
}

func TestEvidenceApplicationBindsWorkspaceConnectorAndRetirement(t *testing.T) {
	first := newEvidenceApplicationFixture(t, "first-workspace")
	second := newEvidenceApplicationFixture(t, "second-workspace")
	rows := make([]connectorapi.CredentialResource, 0, 2)
	readers := make([]connectorapi.CredentialResourceReader, 0, 2)
	for index, fixture := range []evidenceApplicationFixture{first, second} {
		store := fixture.application.DataRuntime(fixture.handle, "first").CredentialResources("journal")
		row, err := store.Create(t.Context(), connectorapi.CreateCredentialResourceInput{
			Name: []string{"first-entry", "second-entry"}[index], ResourceType: "domain.v1", PublicData: `{"status":"confirmed"}`, Secret: struct{}{},
		})
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
		readers = append(readers, fixture.reader(t, "first"))
		if _, err := fixture.reader(t, "second").Get(t.Context(), row.ID); !errors.Is(err, connectorapi.ErrCredentialResourceNotFound) {
			t.Fatalf("evidence read another connector's resource: %v", err)
		}
	}
	for index, reader := range readers {
		got, err := reader.Get(t.Context(), rows[index].ID)
		if err != nil || got.Name != rows[index].Name {
			t.Fatalf("workspace-bound read: %#v %v", got, err)
		}
	}
	if err := first.component.WorkspaceOwner().DiscardWorkspace(first.handle, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := readers[0].Get(t.Context(), rows[0].ID); err == nil {
		t.Fatal("retired workspace left evidence reader usable")
	}
	before := first.provider.evidenceCapabilityProvider.calls
	if result, err := first.application.CleanupEvidenceCapabilities(first.handle, "first"); err == nil || result != nil || first.provider.evidenceCapabilityProvider.calls != before {
		t.Fatal("retired workspace invoked evidence provider")
	}
	if got, err := readers[1].Get(t.Context(), rows[1].ID); err != nil || got.Name != rows[1].Name {
		t.Fatalf("retiring one workspace invalidated the other: %#v %v", got, err)
	}
}
