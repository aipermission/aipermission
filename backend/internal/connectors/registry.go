package connectors

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Registry stores connector implementations by kind.
type Registry struct {
	mu     sync.RWMutex
	byKind map[string]Connector
}

// Catalog is the immutable connector lookup surface used after composition.
// Registration belongs only to the bootstrap Registry builder.
type Catalog interface {
	Get(kind string) (Connector, bool)
	List() []ConnectorInfo
}

type catalogSnapshot struct {
	byKind map[string]Connector
}

// NewRegistry creates an empty connector registry.
func NewRegistry() *Registry {
	return &Registry{byKind: make(map[string]Connector)}
}

// Register adds one connector. Connector kinds are stable lowercase
// identifiers such as "postgres", "redis", or "http_recipe".
func (r *Registry) Register(connector Connector) error {
	if r == nil {
		return fmt.Errorf("connector registry is not configured")
	}
	if connector == nil {
		return fmt.Errorf("connector is nil")
	}
	kind := connector.Kind()
	if !ValidIdentifier(kind) {
		return fmt.Errorf("invalid connector kind %q", kind)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byKind == nil {
		r.byKind = make(map[string]Connector)
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
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	connector, ok := r.byKind[kind]
	return connector, ok
}

// List returns connector metadata in stable order.
func (r *Registry) List() []ConnectorInfo {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return connectorInfos(r.byKind)
}

// Snapshot returns an immutable copy suitable for runtime composition.
func (r *Registry) Snapshot() Catalog {
	if r == nil {
		return catalogSnapshot{byKind: map[string]Connector{}}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	byKind := make(map[string]Connector, len(r.byKind))
	for kind, connector := range r.byKind {
		byKind[kind] = connector
	}
	return catalogSnapshot{byKind: byKind}
}

// SnapshotCatalog copies any read-only catalog into an immutable runtime view.
func SnapshotCatalog(source Catalog) (Catalog, error) {
	if source == nil {
		return catalogSnapshot{byKind: map[string]Connector{}}, nil
	}
	if registry, ok := source.(*Registry); ok {
		return registry.Snapshot(), nil
	}
	infos := source.List()
	byKind := make(map[string]Connector, len(infos))
	for _, info := range infos {
		connector, ok := source.Get(info.Kind)
		if !ok || connector == nil {
			return nil, fmt.Errorf("connector catalog kind %q is missing", info.Kind)
		}
		if connector.Kind() != info.Kind {
			return nil, fmt.Errorf("connector catalog kind %q resolves connector %q", info.Kind, connector.Kind())
		}
		if _, exists := byKind[info.Kind]; exists {
			return nil, fmt.Errorf("connector catalog kind %q is duplicated", info.Kind)
		}
		byKind[info.Kind] = connector
	}
	return catalogSnapshot{byKind: byKind}, nil
}

func (snapshot catalogSnapshot) Get(kind string) (Connector, bool) {
	connector, ok := snapshot.byKind[kind]
	return connector, ok
}

func (snapshot catalogSnapshot) List() []ConnectorInfo {
	return connectorInfos(snapshot.byKind)
}

func connectorInfos(byKind map[string]Connector) []ConnectorInfo {
	infos := make([]ConnectorInfo, 0, len(byKind))
	for kind, connector := range byKind {
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
	actions, err := GetActionDefinitions(context.Background(), connector, baselineTarget, baselineProfile)
	if err != nil {
		return fmt.Errorf("connector %q actions: %w", kind, err)
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
			exemplarActions, err := GetActionDefinitions(context.Background(), connector, target, exemplarProfile)
			if err != nil {
				return fmt.Errorf("connector %q exemplar actions: %w", kind, err)
			}
			if !ActionDefinitionsEqual(baselineActions, exemplarActions) {
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

// ActionDefinitionsEqual compares connector action contracts independent of declaration order.
func ActionDefinitionsEqual(left []ActionDefinition, right []ActionDefinition) bool {
	canonicalLeft := canonicalActionDefinitions(left)
	canonicalRight := canonicalActionDefinitions(right)
	if len(canonicalLeft) != len(canonicalRight) {
		return false
	}
	for index := range canonicalLeft {
		if !equalActionDefinition(canonicalLeft[index], canonicalRight[index]) {
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
		left.MaxInputBytes != right.MaxInputBytes ||
		!reflect.DeepEqual(EffectiveRetryPolicy(left), EffectiveRetryPolicy(right)) ||
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
	if !reflect.DeepEqual(left.OutputHint.TemporaryCapabilityFields, right.OutputHint.TemporaryCapabilityFields) {
		return false
	}
	if !reflect.DeepEqual(left.SensitiveInputFields, right.SensitiveInputFields) {
		return false
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
		left.PreserveWhitespace != right.PreserveWhitespace ||
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
