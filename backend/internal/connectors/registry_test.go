package connectors

import (
	"context"
	"testing"
)

type fakeConnector struct {
	kind              string
	label             string
	version           string
	targetSchema      Schema
	credentialSchemas []CredentialSchema
}

func (f fakeConnector) Kind() string                          { return f.kind }
func (f fakeConnector) Label() string                         { return f.label }
func (f fakeConnector) Version() string                       { return f.version }
func (f fakeConnector) TargetSchema() Schema                  { return f.targetSchema }
func (f fakeConnector) CredentialSchemas() []CredentialSchema { return f.credentialSchemas }
func (f fakeConnector) GetHelp(context.Context, TargetView) (ConnectorHelp, error) {
	return ConnectorHelp{ConnectorID: f.kind, Connector: f.label}, nil
}
func (f fakeConnector) GetActionList(context.Context, TargetView, CredentialProfileView) ([]ActionDefinition, error) {
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

func TestRegistryRejectsInvalidConnectorContract(t *testing.T) {
	tests := []struct {
		name      string
		connector fakeConnector
	}{
		{
			name: "secret target field",
			connector: fakeConnector{
				kind: "api",
				targetSchema: Schema{Fields: []Field{
					{Name: "token", Type: FieldSecret, Secret: true},
				}},
			},
		},
		{
			name: "credential secret type missing secret flag",
			connector: fakeConnector{
				kind: "api",
				credentialSchemas: []CredentialSchema{{
					Kind: "api_key",
					Schema: Schema{Fields: []Field{
						{Name: "token", Type: FieldSecret, Required: true},
					}},
				}},
			},
		},
		{
			name: "credential secret default",
			connector: fakeConnector{
				kind: "api",
				credentialSchemas: []CredentialSchema{{
					Kind: "api_key",
					Schema: Schema{Fields: []Field{
						{Name: "token", Type: FieldSecret, Secret: true, Default: "leaked-token"},
					}},
				}},
			},
		},
		{
			name: "duplicate credential kind",
			connector: fakeConnector{
				kind: "api",
				credentialSchemas: []CredentialSchema{
					{Kind: "api_key"},
					{Kind: "api_key"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			if err := registry.Register(tt.connector); err == nil {
				t.Fatal("expected invalid connector contract error")
			}
		})
	}
}

func TestRegistryAcceptsValidCredentialSchema(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(fakeConnector{
		kind: "api",
		credentialSchemas: []CredentialSchema{{
			Kind: "api_key",
			Schema: Schema{Fields: []Field{
				{Name: "token", Type: FieldSecret, Secret: true, Required: true},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("register valid connector: %v", err)
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
