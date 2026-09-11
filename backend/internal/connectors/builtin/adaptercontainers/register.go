package adaptercontainers

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	dockerapiadapter "github.com/aipermission/aipermission/backend/internal/connectors/docker/apiadapter"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	kubernetesapiadapter "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes/apiadapter"
)

func Register(registry *connectorapi.Registry) error {
	if err := registry.Register(dockerconnector.Kind, dockerapiadapter.New()); err != nil {
		return err
	}
	return registry.Register(kubernetesconnector.Kind, kubernetesapiadapter.New())
}
