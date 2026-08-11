package conformance_test

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	redisconnector "github.com/aipermission/aipermission/backend/internal/connectors/redis"
)

func TestValkeyRealService(t *testing.T) {
	requireConformance(t)
	connector := redisconnector.New()
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID: 2, Ref: "redis:2:2", ConnectorKind: redisconnector.Kind, Name: "conformance-valkey",
			Config: map[string]any{
				"server_family":   redisconnector.ServerFamilyValkey,
				"connection_mode": "direct",
				"host":            fixtureHost("AIPERMISSION_VALKEY_HOST", "127.0.0.1"),
				"port":            fixturePort(t, "AIPERMISSION_VALKEY_PORT", 6379),
				"database":        0,
			},
		},
		Profile: connectors.CredentialProfileView{
			ID: 2, TargetID: 2, ConnectorKind: redisconnector.Kind, Kind: "username_password", Label: "conformance",
			Public: map[string]any{},
		},
		Secrets:      fixtureSecrets{"password": "conformance-only"},
		Capabilities: fixtureCapabilities{},
	}

	assertConnection(t, connector, runtime)
	const key = "aipermission:conformance"
	executeAction(t, connector, runtime, redisconnector.ActionSetString, map[string]any{
		"key": key, "value": "valkey-conformance", "ttl_seconds": 60,
	})
	result := executeAction(t, connector, runtime, redisconnector.ActionGetKey, map[string]any{"key": key})
	assertResultContains(t, result, "valkey-conformance")
	executeAction(t, connector, runtime, redisconnector.ActionDeleteKeys, map[string]any{"keys": []any{key}})
}
