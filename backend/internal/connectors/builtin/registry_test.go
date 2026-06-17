package builtin

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

func TestNewRegistryIncludesBuiltInConnectors(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	postgres, ok := registry.Get(postgresconnector.Kind)
	if !ok {
		t.Fatal("expected postgres connector")
	}
	if postgres.Label() != postgresconnector.Label {
		t.Fatalf("postgres label = %q", postgres.Label())
	}

	connector, ok := registry.Get(sshconnector.Kind)
	if !ok {
		t.Fatal("expected ssh connector")
	}
	if connector.Label() != sshconnector.Label {
		t.Fatalf("label = %q", connector.Label())
	}

	infos := registry.List()
	if len(infos) != 2 || infos[0].Kind != postgresconnector.Kind || infos[1].Kind != sshconnector.Kind {
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

func TestBuiltInConnectorPrepareActionsAreDeterministic(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	for _, info := range registry.List() {
		connector, ok := registry.Get(info.Kind)
		if !ok {
			t.Fatalf("connector %q missing from registry", info.Kind)
		}
		target, profile, inputs := builtInDeterminismSamples(t, info.Kind)
		actions, err := connector.GetActionList(context.Background(), target, profile)
		if err != nil {
			t.Fatalf("%s action list: %v", info.Kind, err)
		}
		if len(actions) != len(inputs) {
			t.Fatalf("%s sample inputs do not cover all actions: actions=%d samples=%d", info.Kind, len(actions), len(inputs))
		}
		for _, action := range actions {
			input, ok := inputs[action.Name]
			if !ok {
				t.Fatalf("%s missing deterministic sample for action %q", info.Kind, action.Name)
			}
			connectortest.AssertPrepareActionDeterministic(t, connector, connectors.ActionRequest{
				Target:     target,
				Profile:    profile,
				ActionName: action.Name,
				Input:      input,
				Reason:     "contract smoke",
			})
		}
	}
}

func builtInDeterminismSamples(t *testing.T, kind string) (connectors.TargetView, connectors.CredentialProfileView, map[string]map[string]any) {
	t.Helper()

	switch kind {
	case postgresconnector.Kind:
		return connectors.TargetView{
				ID:            1,
				Ref:           "postgres:1:10",
				ConnectorKind: postgresconnector.Kind,
				Name:          "main-db",
				Config:        map[string]any{"database": "appdb"},
			}, connectors.CredentialProfileView{
				ID:            10,
				TargetID:      1,
				ConnectorKind: postgresconnector.Kind,
				Kind:          "username_password",
				Label:         "readonly",
			}, map[string]map[string]any{
				postgresconnector.ActionGetSchemas:    {},
				postgresconnector.ActionGetTables:     {"schema": "public", "include_system": false},
				postgresconnector.ActionDescribeTable: {"schema": "public", "table": "users"},
				postgresconnector.ActionQueryReadonly: {"sql": "select 1", "max_rows": 1},
			}
	case sshconnector.Kind:
		return connectors.TargetView{
				ID:            2,
				Ref:           "ssh:2:20",
				ConnectorKind: sshconnector.Kind,
				Name:          "worker",
			}, connectors.CredentialProfileView{
				ID:            20,
				TargetID:      2,
				ConnectorKind: sshconnector.Kind,
				Kind:          "private_key",
				Label:         "root",
			}, map[string]map[string]any{
				sshconnector.ActionExec:                  {"command": "echo aipermission"},
				sshconnector.ActionReadConsole:           {"tail_bytes": 2048},
				sshconnector.ActionRestartConsoleSession: {},
				sshconnector.ActionBrowseRemoteFiles:     {"path": "~"},
				sshconnector.ActionStartFileDownload:     {"remote_paths": []any{"/etc/hosts"}, "archive_name": "hosts.zip"},
			}
	default:
		t.Fatalf("missing built-in deterministic samples for connector %q", kind)
	}
	return connectors.TargetView{}, connectors.CredentialProfileView{}, nil
}
