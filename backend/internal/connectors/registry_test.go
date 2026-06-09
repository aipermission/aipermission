package connectors

import (
	"context"
	"testing"
)

type fakeConnector struct {
	kind    string
	label   string
	version string
}

func (f fakeConnector) Kind() string                          { return f.kind }
func (f fakeConnector) Label() string                         { return f.label }
func (f fakeConnector) Version() string                       { return f.version }
func (f fakeConnector) TargetSchema() Schema                  { return Schema{} }
func (f fakeConnector) CredentialSchemas() []CredentialSchema { return nil }
func (f fakeConnector) GetHelp(context.Context, TargetView) (ConnectorHelp, error) {
	return ConnectorHelp{ConnectorID: f.kind, Connector: f.label}, nil
}
func (f fakeConnector) GetActionList(context.Context, TargetView) ([]ActionDefinition, error) {
	return []ActionDefinition{{Name: "example", Risk: RiskRead}}, nil
}
func (f fakeConnector) PrepareAction(context.Context, ActionRequest) (PreparedAction, error) {
	return PreparedAction{ConnectorKind: f.kind, ActionName: "example"}, nil
}
func (f fakeConnector) ExecuteAction(context.Context, RuntimeContext, PreparedAction) (ActionResult, error) {
	return ActionResult{Status: ResultCompleted}, nil
}

func TestRegistryRegisterAndList(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(fakeConnector{kind: "postgres", label: "Postgres", version: "0.1"}); err != nil {
		t.Fatalf("register postgres: %v", err)
	}
	if err := registry.Register(fakeConnector{kind: "ssh", label: "SSH", version: "0.1"}); err != nil {
		t.Fatalf("register ssh: %v", err)
	}

	if connector, ok := registry.Get("ssh"); !ok || connector.Label() != "SSH" {
		t.Fatalf("expected ssh connector, got %v ok=%v", connector, ok)
	}

	got := registry.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 connectors, got %d", len(got))
	}
	if got[0].Kind != "postgres" || got[1].Kind != "ssh" {
		t.Fatalf("expected stable kind order, got %#v", got)
	}
}

func TestRegistryRejectsInvalidConnector(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(nil); err == nil {
		t.Fatal("expected nil connector error")
	}
	if err := registry.Register(fakeConnector{kind: "Bad-Kind"}); err == nil {
		t.Fatal("expected invalid kind error")
	}
}

func TestRegistryRejectsDuplicateKind(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(fakeConnector{kind: "ssh", label: "SSH", version: "0.1"}); err != nil {
		t.Fatalf("register ssh: %v", err)
	}
	if err := registry.Register(fakeConnector{kind: "ssh", label: "SSH again", version: "0.2"}); err == nil {
		t.Fatal("expected duplicate kind error")
	}
}

func TestValidIdentifier(t *testing.T) {
	tests := map[string]bool{
		"ssh":         true,
		"postgres":    true,
		"http_recipe": true,
		"redis2":      true,
		"":            false,
		"SSH":         false,
		"2redis":      false,
		"http-recipe": false,
		"api.recipe":  false,
	}

	for value, want := range tests {
		if got := ValidIdentifier(value); got != want {
			t.Fatalf("ValidIdentifier(%q) = %v, want %v", value, got, want)
		}
	}
}
