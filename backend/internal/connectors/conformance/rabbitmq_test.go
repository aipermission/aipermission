package conformance_test

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	rabbitmqconnector "github.com/aipermission/aipermission/backend/internal/connectors/rabbitmq"
)

func TestRabbitMQRealService(t *testing.T) {
	requireConformance(t)
	connector := rabbitmqconnector.New()
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID: 3, Ref: "rabbitmq:3:3", ConnectorKind: rabbitmqconnector.Kind, Name: "conformance-rabbitmq",
			Config: map[string]any{
				"connection_mode": "direct",
				"scheme":          "http",
				"host":            fixtureHost("AIPERMISSION_RABBITMQ_HOST", "127.0.0.1"),
				"port":            fixturePort(t, "AIPERMISSION_RABBITMQ_PORT", 15672),
				"vhost":           "/",
			},
		},
		Profile: connectors.CredentialProfileView{
			ID: 3, TargetID: 3, ConnectorKind: rabbitmqconnector.Kind, Kind: "username_password", Label: "conformance",
			Public: map[string]any{"username": "aipermission"},
		},
		Secrets:      fixtureSecrets{"password": "conformance-only"},
		Capabilities: fixtureCapabilities{},
	}

	assertConnection(t, connector, runtime)
	result := executeAction(t, connector, runtime, rabbitmqconnector.ActionOverview, nil)
	assertResultContains(t, result, "rabbitmq_version")
	executeAction(t, connector, runtime, rabbitmqconnector.ActionListVhosts, nil)
}
