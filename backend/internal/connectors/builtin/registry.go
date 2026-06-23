// Package builtin registers connector implementations shipped with
// AIPermission.
package builtin

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	_ "github.com/aipermission/aipermission/backend/internal/connectors/docker/apiadapter"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	_ "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes/apiadapter"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	rabbitmqconnector "github.com/aipermission/aipermission/backend/internal/connectors/rabbitmq"
	redisconnector "github.com/aipermission/aipermission/backend/internal/connectors/redis"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	_ "github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter"
)

// RegisterAll adds all built-in connectors to the provided registry.
func RegisterAll(registry *connectors.Registry) error {
	for _, connector := range []connectors.Connector{
		dockerconnector.New(),
		kubernetesconnector.New(),
		postgresconnector.New(),
		rabbitmqconnector.New(),
		redisconnector.New(),
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
