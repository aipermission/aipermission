package connectors

import (
	"context"
	"strings"
	"testing"
)

type fakeConnector struct {
	kind              string
	label             string
	version           string
	targetSchema      Schema
	credentialSchemas []CredentialSchema
	actionDefinitions []ActionDefinition
	dynamicActions    bool
	profileActions    bool
	reverseActions    bool
}

func (f fakeConnector) Kind() string { return f.kind }
func (f fakeConnector) Label() string {
	if f.label == "__empty__" {
		return ""
	}
	if f.label != "" {
		return f.label
	}
	return "Test connector"
}
func (f fakeConnector) Version() string {
	if f.version == "__empty__" {
		return ""
	}
	if f.version != "" {
		return f.version
	}
	return "0.1"
}
func (f fakeConnector) TargetSchema() Schema                  { return f.targetSchema }
func (f fakeConnector) CredentialSchemas() []CredentialSchema { return f.credentialSchemas }
func (f fakeConnector) GetHelp(context.Context, TargetView) (ConnectorHelp, error) {
	return ConnectorHelp{ConnectorID: f.kind, Connector: f.label}, nil
}
func (f fakeConnector) GetActionList(_ context.Context, target TargetView, profile CredentialProfileView) ([]ActionDefinition, error) {
	if f.dynamicActions && target.Name != "" {
		return []ActionDefinition{{Name: "example_dynamic", Label: "Example dynamic", Description: "Example dynamic action.", Risk: RiskRead}}, nil
	}
	if f.profileActions && profile.Kind == "oauth" {
		return []ActionDefinition{{Name: "oauth_only", Label: "OAuth only", Description: "OAuth-only action.", Risk: RiskRead}}, nil
	}
	if f.actionDefinitions != nil {
		if f.reverseActions && target.Name != "" {
			reversed := append([]ActionDefinition(nil), f.actionDefinitions...)
			for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
				reversed[i], reversed[j] = reversed[j], reversed[i]
			}
			return reversed, nil
		}
		return f.actionDefinitions, nil
	}
	return []ActionDefinition{{Name: "example", Label: "Example", Description: "Example action.", Risk: RiskRead}}, nil
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
	if err := registry.Register(fakeConnector{kind: "api", label: "__empty__"}); err == nil || !strings.Contains(err.Error(), "label is required") {
		t.Fatalf("expected missing label error, got %v", err)
	}
	if err := registry.Register(fakeConnector{kind: "api", version: "__empty__"}); err == nil || !strings.Contains(err.Error(), "version is required") {
		t.Fatalf("expected missing version error, got %v", err)
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
		{
			name: "invalid action definition",
			connector: fakeConnector{
				kind:              "api",
				actionDefinitions: []ActionDefinition{{Name: "Bad-Action", Risk: RiskRead}},
			},
		},
		{
			name: "duplicate action definition",
			connector: fakeConnector{
				kind: "api",
				actionDefinitions: []ActionDefinition{
					{Name: "call", Label: "Call", Description: "Call a test action.", Risk: RiskRead},
					{Name: "call", Label: "Call again", Description: "Call another test action.", Risk: RiskRead},
				},
			},
		},
		{
			name: "secret action input",
			connector: fakeConnector{
				kind: "api",
				actionDefinitions: []ActionDefinition{{
					Name:        "call",
					Label:       "Call",
					Description: "Call a test action.",
					Risk:        RiskRead,
					InputSchema: Schema{Fields: []Field{
						{Name: "token", Type: FieldSecret, Secret: true},
					}},
				}},
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

func TestRegistryRejectsTargetDependentActionCatalog(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(fakeConnector{
		kind:           "api",
		dynamicActions: true,
		credentialSchemas: []CredentialSchema{{
			Kind: "api_key",
			Schema: Schema{Fields: []Field{
				{Name: "token", Type: FieldSecret, Secret: true, Required: true},
			}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "action list must be stable") {
		t.Fatalf("expected action stability error, got %v", err)
	}
}

func TestRegistryRejectsCredentialKindDependentActionCatalog(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(fakeConnector{
		kind:           "api",
		profileActions: true,
		credentialSchemas: []CredentialSchema{
			{
				Kind: "api_key",
				Schema: Schema{Fields: []Field{
					{Name: "token", Type: FieldSecret, Secret: true, Required: true},
				}},
			},
			{
				Kind: "oauth",
				Schema: Schema{Fields: []Field{
					{Name: "token", Type: FieldSecret, Secret: true, Required: true},
				}},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "action list must be stable") {
		t.Fatalf("expected action stability error, got %v", err)
	}
}

func TestRegistryAcceptsStableActionCatalogWithDifferentOrder(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(fakeConnector{
		kind:           "api",
		reverseActions: true,
		actionDefinitions: []ActionDefinition{
			{Name: "alpha", Label: "Alpha", Description: "Alpha action.", Risk: RiskRead},
			{Name: "beta", Label: "Beta", Description: "Beta action.", Risk: RiskRead},
		},
		credentialSchemas: []CredentialSchema{{
			Kind: "api_key",
			Schema: Schema{Fields: []Field{
				{Name: "token", Type: FieldSecret, Secret: true, Required: true},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("expected stable action catalog to register: %v", err)
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
