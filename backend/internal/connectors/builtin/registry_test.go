package builtin

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

func TestNewRegistryIncludesSSHConnector(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	connector, ok := registry.Get(sshconnector.Kind)
	if !ok {
		t.Fatal("expected ssh connector")
	}
	if connector.Label() != sshconnector.Label {
		t.Fatalf("label = %q", connector.Label())
	}

	infos := registry.List()
	if len(infos) != 1 || infos[0].Kind != sshconnector.Kind {
		t.Fatalf("unexpected connector list: %#v", infos)
	}
}

func TestRegisterAllPropagatesRegistryErrors(t *testing.T) {
	registry := connectors.NewRegistry()
	if err := registry.Register(sshconnector.New()); err != nil {
		t.Fatalf("seed ssh: %v", err)
	}

	if err := RegisterAll(registry); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}
