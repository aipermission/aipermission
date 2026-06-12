package connectors

import (
	"fmt"
	"regexp"
	"sort"
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
// identifiers such as "ssh", "postgres", or "http_recipe".
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
	return nil
}
