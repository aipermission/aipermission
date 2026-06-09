// Package builtin registers connector implementations shipped with
// AIPermission.
package builtin

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

// RegisterAll adds all built-in connectors to the provided registry.
func RegisterAll(registry *connectors.Registry) error {
	for _, connector := range []connectors.Connector{
		sshconnector.New(),
	} {
		if err := registry.Register(connector); err != nil {
			return err
		}
	}
	return nil
}

// NewRegistry returns a registry populated with all built-in connectors.
func NewRegistry() (*connectors.Registry, error) {
	registry := connectors.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		return nil, err
	}
	return registry, nil
}
