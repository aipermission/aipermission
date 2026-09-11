package adapterresources

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	s3apiadapter "github.com/aipermission/aipermission/backend/internal/connectors/s3/apiadapter"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	sshapiadapter "github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter"
)

func Register(registry *connectorapi.Registry) error {
	if err := registry.Register(s3connector.Kind, s3apiadapter.New()); err != nil {
		return err
	}
	return registry.Register(sshconnector.Kind, sshapiadapter.New())
}
