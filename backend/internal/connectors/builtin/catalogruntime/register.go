package catalogruntime

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

func Register(registry *connectors.Registry) error {
	for _, connector := range []connectors.Connector{
		dockerconnector.New(),
		kubernetesconnector.New(),
		s3connector.New(),
		sshconnector.New(),
	} {
		if err := registry.Register(connector); err != nil {
			return err
		}
	}
	return nil
}
