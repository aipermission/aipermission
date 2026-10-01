package adapterresources

import (
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	postgresapiadapter "github.com/aipermission/aipermission/backend/internal/connectors/postgres/apiadapter"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	s3apiadapter "github.com/aipermission/aipermission/backend/internal/connectors/s3/apiadapter"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	sshapiadapter "github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func Register(registry *connectorapi.Registry) error {
	if err := registry.Register(postgresconnector.Kind, postgresapiadapter.New()); err != nil {
		return err
	}
	if err := registry.Register(s3connector.Kind, s3apiadapter.New()); err != nil {
		return err
	}
	return registry.Register(sshconnector.Kind, sshapiadapter.New())
}
