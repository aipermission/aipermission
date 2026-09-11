package catalogdata

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	clickhouseconnector "github.com/aipermission/aipermission/backend/internal/connectors/clickhouse"
	kafkaconnector "github.com/aipermission/aipermission/backend/internal/connectors/kafka"
	mailconnector "github.com/aipermission/aipermission/backend/internal/connectors/mail"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	rabbitmqconnector "github.com/aipermission/aipermission/backend/internal/connectors/rabbitmq"
	redisconnector "github.com/aipermission/aipermission/backend/internal/connectors/redis"
)

func Register(registry *connectors.Registry) error {
	for _, connector := range []connectors.Connector{
		clickhouseconnector.New(),
		kafkaconnector.New(),
		mailconnector.New(),
		postgresconnector.New(),
		rabbitmqconnector.New(),
		redisconnector.New(),
	} {
		if err := registry.Register(connector); err != nil {
			return err
		}
	}
	return nil
}
