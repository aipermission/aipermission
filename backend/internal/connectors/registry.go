package connectors

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Registry stores connector implementations by kind.
type Registry struct {
	byKind map[string]Connector
}

// NewRegistry creates an empty connector registry.
func NewRegistry() *Registry {
	return &Registry{byKind: make(map[string]Connector)}
}

// Register adds one connector. Connector kinds are stable lowercase
// identifiers such as "postgres", "redis", or "http_recipe".
func (r *Registry) Register(connector Connector) error {
	if connector == nil {
		return fmt.Errorf("connector is nil")
	}
	kind := connector.Kind()
	if !ValidIdentifier(kind) {
		return fmt.Errorf("invalid connector kind %q", kind)
	}
	if _, exists := r.byKind[kind]; exists {
		return fmt.Errorf("connector kind %q already registered", kind)
	}
	if err := ValidateConnectorContract(connector); err != nil {
		return err
	}
	r.byKind[kind] = connector
	return nil
}

// Get returns a connector by kind.
func (r *Registry) Get(kind string) (Connector, bool) {
	connector, ok := r.byKind[kind]
	return connector, ok
}

// List returns connector metadata in stable order.
func (r *Registry) List() []ConnectorInfo {
	infos := make([]ConnectorInfo, 0, len(r.byKind))
	for kind, connector := range r.byKind {
		infos = append(infos, ConnectorInfo{
			Kind:    kind,
			Label:   connector.Label(),
			Version: connector.Version(),
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Kind < infos[j].Kind
	})
	return infos
}

// ConnectorInfo is safe to expose in local UI/API metadata.
type ConnectorInfo struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Version string `json:"version"`
}

// ValidIdentifier validates connector kinds and action names.
func ValidIdentifier(value string) bool {
	return identifierPattern.MatchString(value)
}

func ValidateConnectorContract(connector Connector) error {
	if connector == nil {
		return fmt.Errorf("connector is nil")
	}
	kind := connector.Kind()
	if !ValidIdentifier(kind) {
		return fmt.Errorf("invalid connector kind %q", kind)
	}
	if strings.TrimSpace(connector.Label()) == "" {
		return fmt.Errorf("connector %q label is required", kind)
	}
	if strings.TrimSpace(connector.Version()) == "" {
		return fmt.Errorf("connector %q version is required", kind)
	}
	if err := ValidateNonSecretSchema(connector.TargetSchema(), kind+" target"); err != nil {
		return err
	}
	seenCredentialKinds := map[string]bool{}
	for _, schema := range connector.CredentialSchemas() {
		if seenCredentialKinds[schema.Kind] {
			return fmt.Errorf("connector %q contains duplicate credential kind %q", kind, schema.Kind)
		}
		seenCredentialKinds[schema.Kind] = true
		if err := ValidateCredentialSchemaDefinition(schema); err != nil {
			return fmt.Errorf("connector %q: %w", kind, err)
		}
	}
	baselineTarget := TargetView{ConnectorKind: kind}
	baselineProfile := CredentialProfileView{ConnectorKind: kind}
	actions, err := connector.GetActionList(context.Background(), baselineTarget, baselineProfile)
	if err != nil {
		return fmt.Errorf("connector %q actions: %w", kind, err)
	}
	if err := ValidateActionDefinitions(actions, kind+" actions"); err != nil {
		return fmt.Errorf("connector %q: %w", kind, err)
	}
	baselineActions := canonicalActionDefinitions(actions)
	profileKinds := []string{""}
	for _, schema := range connector.CredentialSchemas() {
		profileKinds = append(profileKinds, schema.Kind)
	}
	exemplarTargets := []TargetView{
		baselineTarget,
		{
			ConnectorKind: kind,
			Name:          "__contract_check__",
			Config:        map[string]any{"__contract_variant": "target"},
		},
	}
	for _, target := range exemplarTargets {
		for _, profileKind := range profileKinds {
			exemplarProfile := CredentialProfileView{
				ConnectorKind: kind,
				Kind:          profileKind,
				Label:         "__contract_check__",
				Public:        map[string]any{"__contract_variant": profileKind},
			}
			exemplarActions, err := connector.GetActionList(context.Background(), target, exemplarProfile)
			if err != nil {
				return fmt.Errorf("connector %q exemplar actions: %w", kind, err)
			}
			if err := ValidateActionDefinitions(exemplarActions, kind+" actions"); err != nil {
				return fmt.Errorf("connector %q: %w", kind, err)
			}
			if !equalActionDefinitions(baselineActions, canonicalActionDefinitions(exemplarActions)) {
				return fmt.Errorf("connector %q action list must be stable for the connector kind", kind)
			}
		}
	}
	return nil
}

func canonicalActionDefinitions(actions []ActionDefinition) []ActionDefinition {
	canonical := append([]ActionDefinition(nil), actions...)
	sort.SliceStable(canonical, func(i, j int) bool {
		return canonical[i].Name < canonical[j].Name
	})
	return canonical
}

func equalActionDefinitions(left []ActionDefinition, right []ActionDefinition) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !equalActionDefinition(left[index], right[index]) {
			return false
		}
	}
	return true
}

func equalActionDefinition(left ActionDefinition, right ActionDefinition) bool {
	if left.Name != right.Name ||
		left.Label != right.Label ||
		left.Description != right.Description ||
		left.Category != right.Category ||
		left.Risk != right.Risk ||
		left.OutputHint.Format != right.OutputHint.Format ||
		left.OutputHint.MaxRows != right.OutputHint.MaxRows ||
		left.OutputHint.MaxBytes != right.OutputHint.MaxBytes {
		return false
	}
	if len(left.OutputHint.SensitiveFields) != len(right.OutputHint.SensitiveFields) {
		return false
	}
	for index := range left.OutputHint.SensitiveFields {
		if left.OutputHint.SensitiveFields[index] != right.OutputHint.SensitiveFields[index] {
			return false
		}
	}
	return equalSchemas(left.InputSchema, right.InputSchema)
}

func equalSchemas(left Schema, right Schema) bool {
	if len(left.Fields) != len(right.Fields) {
		return false
	}
	for index := range left.Fields {
		if !equalFields(left.Fields[index], right.Fields[index]) {
			return false
		}
	}
	return true
}

func equalFields(left Field, right Field) bool {
	if left.Name != right.Name ||
		left.Label != right.Label ||
		left.Type != right.Type ||
		left.Required != right.Required ||
		left.Secret != right.Secret ||
		left.Description != right.Description ||
		!reflect.DeepEqual(left.Default, right.Default) ||
		len(left.Options) != len(right.Options) {
		return false
	}
	for index := range left.Options {
		if left.Options[index] != right.Options[index] {
			return false
		}
	}
	return true
}
