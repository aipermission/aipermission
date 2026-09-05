package conformance_test

import (
	"fmt"
	"testing"
	"time"

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

	// A small COUNT must traverse the service's real cursor rather than repeat
	// the first page. TTLs bound fixture lifetime if the test is interrupted.
	prefix := fmt.Sprintf("aipermission:conformance:scan:%d:", time.Now().UnixNano())
	want := map[string]bool{}
	for index := 0; index < 32; index++ {
		key := fmt.Sprintf("%s%d", prefix, index)
		want[key] = true
		executeAction(t, connector, runtime, redisconnector.ActionSetString, map[string]any{"key": key, "value": "scan", "ttl_seconds": 60})
	}
	cursor := "0"
	seen := map[string]bool{}
	for page := 0; ; page++ {
		if page == 100 {
			t.Fatal("scan failed to complete")
		}
		result := executeAction(t, connector, runtime, redisconnector.ActionScanKeys, map[string]any{"pattern": prefix + "*", "cursor": cursor, "limit": 1})
		output := result.Output.(map[string]any)
		for _, key := range output["keys"].([]string) {
			seen[key] = true
		}
		cursor = output["next_cursor"].(string)
		if cursor == "0" {
			break
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("scan returned %d of %d fixture keys", len(seen), len(want))
	}
	for key := range want {
		if !seen[key] {
			t.Fatalf("scan lost %q", key)
		}
		executeAction(t, connector, runtime, redisconnector.ActionDeleteKeys, map[string]any{"keys": []any{key}})
	}
}
