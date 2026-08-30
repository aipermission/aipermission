// Package builtin registers connector implementations shipped with
// AIPermission.
package builtin

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	clickhouseconnector "github.com/aipermission/aipermission/backend/internal/connectors/clickhouse"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	dockerapiadapter "github.com/aipermission/aipermission/backend/internal/connectors/docker/apiadapter"
	kafkaconnector "github.com/aipermission/aipermission/backend/internal/connectors/kafka"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	kubernetesapiadapter "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes/apiadapter"
	mailconnector "github.com/aipermission/aipermission/backend/internal/connectors/mail"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	rabbitmqconnector "github.com/aipermission/aipermission/backend/internal/connectors/rabbitmq"
	redisconnector "github.com/aipermission/aipermission/backend/internal/connectors/redis"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	s3apiadapter "github.com/aipermission/aipermission/backend/internal/connectors/s3/apiadapter"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	sshapiadapter "github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter"
)

type Catalog struct {
	Connectors *connectors.Registry
	Adapters   *connectorapi.Registry
}

// RegisterAll adds all built-in connectors to the provided registry.
func RegisterAll(registry *connectors.Registry) error {
	for _, connector := range []connectors.Connector{
		clickhouseconnector.New(),
		dockerconnector.New(),
		kafkaconnector.New(),
		kubernetesconnector.New(),
		mailconnector.New(),
		postgresconnector.New(),
		rabbitmqconnector.New(),
		redisconnector.New(),
		s3connector.New(),
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

func RegisterAdapters(registry *connectorapi.Registry) error {
	registrations := []struct {
		kind    string
		adapter connectorapi.Adapter
	}{
		{kind: dockerconnector.Kind, adapter: dockerapiadapter.New()},
		{kind: kubernetesconnector.Kind, adapter: kubernetesapiadapter.New()},
		{kind: s3connector.Kind, adapter: s3apiadapter.New()},
		{kind: sshconnector.Kind, adapter: sshapiadapter.New()},
	}
	for _, registration := range registrations {
		if err := registry.Register(registration.kind, registration.adapter); err != nil {
			return err
		}
	}
	return nil
}

func NewAdapterRegistry() (*connectorapi.Registry, error) {
	registry := connectorapi.NewRegistry()
	if err := RegisterAdapters(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func NewCatalog() (Catalog, error) {
	connectorRegistry, err := NewRegistry()
	if err != nil {
		return Catalog{}, err
	}
	adapterRegistry, err := NewAdapterRegistry()
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Connectors: connectorRegistry, Adapters: adapterRegistry}, nil
}
