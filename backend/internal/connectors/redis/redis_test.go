package redisconnector

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestTargetSchemaExposesRedisAndValkeyProducts(t *testing.T) {
	schema := Connector{}.TargetSchema()
	if len(schema.Fields) == 0 || schema.Fields[0].Name != "server_family" {
		t.Fatalf("server family field missing: %#v", schema.Fields)
	}
	field := schema.Fields[0]
	if field.Default != ServerFamilyRedis {
		t.Fatalf("default server family = %#v", field.Default)
	}
	if !reflect.DeepEqual(field.Options, []connectors.FieldOption{
		{Value: ServerFamilyRedis, Label: "Redis"},
		{Value: ServerFamilyValkey, Label: "Valkey"},
	}) {
		t.Fatalf("server family options = %#v", field.Options)
	}
}

func TestRedisTLSConfigPreservesSavedValuesAndSecuresNewRemoteTargets(t *testing.T) {
	plaintextTargets := []connectors.TargetView{
		{Config: map[string]any{"host": "cache.internal"}},
		{Config: map[string]any{"tls_mode": "disable", "host": "cache.internal"}},
		{Config: map[string]any{"tls_mode": "auto", "connection_mode": "direct", "host": "127.0.0.1"}},
		{Config: map[string]any{"tls_mode": "auto", "connection_mode": "over_ssh", "host": "cache.internal"}},
	}
	for _, target := range plaintextTargets {
		if config := redisTLSConfig(target); config != nil {
			t.Fatalf("plaintext TLS config = %#v for target %#v, want nil", config, target.Config)
		}
	}
	verifiedTargets := []connectors.TargetView{
		{Config: map[string]any{"tls_mode": "verify_full", "connection_mode": "over_ssh", "host": "cache.internal"}},
		{Config: map[string]any{"tls_mode": "auto", "connection_mode": "direct", "host": "cache.internal"}},
	}
	for _, target := range verifiedTargets {
		config := redisTLSConfig(target)
		if config == nil || config.ServerName != "cache.internal" || config.MinVersion != tls.VersionTLS12 {
			t.Fatalf("unexpected verified TLS config for %#v: %#v", target.Config, config)
		}
	}
}

func TestClassifyRedisTLSError(t *testing.T) {
	if got := classifyRedisTestError(errors.New("redis TLS handshake: x509: certificate signed by unknown authority")); got != connectors.TestFailedTLS {
		t.Fatalf("status = %q, want %q", got, connectors.TestFailedTLS)
	}
}

func TestSetStringMarksOnlyValueAsSensitiveInput(t *testing.T) {
	actions, err := Connector{}.GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatalf("actions: %v", err)
	}
	for _, action := range actions {
		if action.Name == ActionSetString {
			if !reflect.DeepEqual(action.SensitiveInputFields, []string{"value"}) {
				t.Fatalf("sensitive fields = %#v", action.SensitiveInputFields)
			}
			return
		}
	}
	t.Fatal("set_string action was not found")
}

func TestRESPReaderRejectsOversizedOrMalformedFrames(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			name:     "bulk string",
			response: fmt.Sprintf("$%d\r\n", maxRESPBulkBytes+1),
			want:     "bulk string exceeds",
		},
		{
			name:     "array",
			response: fmt.Sprintf("*%d\r\n", maxRESPArrayItems+1),
			want:     "array exceeds",
		},
		{
			name:     "negative bulk string",
			response: "$-2\r\n",
			want:     "invalid redis bulk string size",
		},
		{
			name:     "negative array",
			response: "*-2\r\n",
			want:     "invalid redis array size",
		},
		{
			name:     "line",
			response: "+" + strings.Repeat("x", maxRESPLineBytes) + "\r\n",
			want:     "response line exceeds",
		},
		{
			name:     "nesting",
			response: strings.Repeat("*1\r\n", maxRESPNestingDepth+2) + "+OK\r\n",
			want:     "response nesting exceeds",
		},
		{
			name: "total bytes",
			response: "*9\r\n" + strings.Repeat(
				fmt.Sprintf("$%d\r\n%s\r\n", maxRESPBulkBytes, strings.Repeat("x", maxRESPBulkBytes)),
				9,
			),
			want: "response exceeds",
		},
		{
			name:     "total values",
			response: fmt.Sprintf("*%d\r\n", maxRESPArrayItems) + strings.Repeat("*1\r\n+OK\r\n", maxRESPArrayItems),
			want:     "response exceeds",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := readRESPValue(bufio.NewReader(strings.NewReader(test.response)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestGetHelpUsesConfiguredServerProduct(t *testing.T) {
	help, err := Connector{}.GetHelp(context.Background(), connectors.TargetView{
		Name:   "cache",
		Config: map[string]any{"server_family": ServerFamilyValkey},
	})
	if err != nil {
		t.Fatalf("get help: %v", err)
	}
	if help.Title != "Valkey target: cache" || help.Connector != Label {
		t.Fatalf("help = %#v", help)
	}
}

func TestExistingTargetWithoutServerFamilyDefaultsToRedis(t *testing.T) {
	target := connectors.TargetView{Name: "existing-cache", Config: map[string]any{"connection_mode": "direct"}}
	if family := serverFamily(target); family != ServerFamilyRedis {
		t.Fatalf("server family = %q", family)
	}
	help, err := Connector{}.GetHelp(context.Background(), target)
	if err != nil {
		t.Fatalf("get help: %v", err)
	}
	if help.Title != "Redis target: existing-cache" {
		t.Fatalf("help title = %q", help.Title)
	}
	prepared, err := Connector{}.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     target,
		Profile:    connectors.CredentialProfileView{Label: "default"},
		ActionName: ActionPing,
		Input:      map[string]any{},
	})
	if err != nil {
		t.Fatalf("prepare action: %v", err)
	}
	if prepared.Title != "Ping Redis" || prepared.ContextMaterial["server_family"] != ServerFamilyRedis {
		t.Fatalf("prepared = %#v", prepared)
	}
}

func TestPrepareActionNormalizesRedisScan(t *testing.T) {
	target := connectors.TargetView{ID: 1, Ref: "redis:1:2", ConnectorKind: Kind, Name: "cache", Config: map[string]any{"server_family": ServerFamilyValkey, "connection_mode": "direct"}}
	profile := connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "username_password", Label: "default"}

	prepared, err := Connector{}.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     target,
		Profile:    profile,
		ActionName: ActionScanKeys,
		Input:      map[string]any{"pattern": "user:*", "limit": 5000},
	})
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}
	if prepared.Payload["limit"] != maxScanLimit {
		t.Fatalf("limit = %#v", prepared.Payload["limit"])
	}
	if prepared.Risk != connectors.RiskRead {
		t.Fatalf("risk = %q", prepared.Risk)
	}
	if prepared.Title != "Scan Valkey keys" {
		t.Fatalf("title = %q", prepared.Title)
	}
	if prepared.ContextMaterial["server_family"] != ServerFamilyValkey {
		t.Fatalf("context material = %#v", prepared.ContextMaterial)
	}
}

func TestExecuteActionUsesNetworkTransportForPing(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		if !reflect.DeepEqual(command, []string{"PING"}) {
			t.Fatalf("command = %#v", command)
		}
		return "+PONG\r\n"
	})
	result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{ActionName: ActionPing})
	if err != nil {
		t.Fatalf("execute ping: %v", err)
	}
	if result.Status != connectors.ResultCompleted || result.Output.(map[string]any)["response"] != "PONG" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteActionScansBoundedKeys(t *testing.T) {
	commands := 0
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		commands++
		if command[0] != "SCAN" {
			t.Fatalf("command = %#v", command)
		}
		return "*2\r\n$1\r\n0\r\n*2\r\n$6\r\nuser:1\r\n$6\r\nuser:2\r\n"
	})
	result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
		ActionName: ActionScanKeys,
		Payload:    map[string]any{"pattern": "user:*", "cursor": "0", "limit": 10},
	})
	if err != nil {
		t.Fatalf("execute scan: %v", err)
	}
	output := result.Output.(map[string]any)
	if got := output["keys"]; !reflect.DeepEqual(got, []string{"user:1", "user:2"}) {
		t.Fatalf("keys = %#v", got)
	}
	if commands != 1 {
		t.Fatalf("commands = %d", commands)
	}
}

func TestConnectionDetectsValkeyServerIdentity(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "PING":
			return "+PONG\r\n"
		case "INFO":
			if !reflect.DeepEqual(command, []string{"INFO", "server"}) {
				t.Fatalf("command = %#v", command)
			}
			return respBulk("# Server\r\nserver_name:valkey\r\nvalkey_version:8.1.3\r\nredis_version:7.2.4\r\n")
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})
	runtime.Target.Config["server_family"] = ServerFamilyValkey

	result, err := Connector{}.TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if result.Status != connectors.TestOK || result.Message != "Valkey connection ok." {
		t.Fatalf("result = %#v", result)
	}
	if result.Details["detected_server_family"] != ServerFamilyValkey ||
		result.Details["server_version"] != "8.1.3" ||
		result.Details["compatibility_version"] != "7.2.4" {
		t.Fatalf("details = %#v", result.Details)
	}
}

func TestConnectionFailsClosedOnSecretProviderError(t *testing.T) {
	providerErr := errors.New("vault unavailable")
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		t.Fatalf("unexpected Redis command after secret-provider failure: %#v", command)
		return ""
	})
	runtime.Secrets = failingSecrets{err: providerErr}

	result, err := Connector{}.TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if result.Status != connectors.TestUnknownError || !strings.Contains(result.Message, "resolve redis password") {
		t.Fatalf("result = %#v", result)
	}
}

func TestConnectionDetectsRedisServerIdentity(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "PING":
			return "+PONG\r\n"
		case "INFO":
			return respBulk("# Server\r\nredis_version:7.2.5\r\n")
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})

	result, err := Connector{}.TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if result.Status != connectors.TestOK || result.Message != "Redis connection ok." {
		t.Fatalf("result = %#v", result)
	}
	if result.Details["detected_server_family"] != ServerFamilyRedis || result.Details["server_version"] != "7.2.5" {
		t.Fatalf("details = %#v", result.Details)
	}
	if result.Details["server_family_match"] != true {
		t.Fatalf("details = %#v", result.Details)
	}
}

func TestConnectionReportsConfiguredServerProductMismatch(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "PING":
			return "+PONG\r\n"
		case "INFO":
			return respBulk("# Server\r\nredis_version:7.2.5\r\n")
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})
	runtime.Target.Config["server_family"] = ServerFamilyValkey

	result, err := Connector{}.TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if result.Status != connectors.TestOK || result.Message != "Redis connection ok; target is configured as Valkey." {
		t.Fatalf("result = %#v", result)
	}
	if result.Details["server_family_match"] != false ||
		result.Details["configured_server_family"] != ServerFamilyValkey ||
		result.Details["detected_server_family"] != ServerFamilyRedis {
		t.Fatalf("details = %#v", result.Details)
	}
}

func TestConnectionAllowsRestrictedProfileWithoutInfoPermission(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "PING":
			return "+PONG\r\n"
		case "INFO":
			return "-NOPERM this user has no permissions to run the 'info' command\r\n"
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})
	runtime.Target.Config["server_family"] = ServerFamilyValkey

	result, err := Connector{}.TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if result.Status != connectors.TestOK || result.Message != "Valkey connection ok." {
		t.Fatalf("result = %#v", result)
	}
	if result.Details["server_detection"] != "unavailable" {
		t.Fatalf("details = %#v", result.Details)
	}
}

func TestValkeyConnectionUsesACLAndSelectedDatabase(t *testing.T) {
	commands := [][]string{}
	var commandsMu sync.Mutex
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		commandsMu.Lock()
		commands = append(commands, append([]string(nil), command...))
		commandsMu.Unlock()
		switch command[0] {
		case "AUTH", "SELECT":
			return "+OK\r\n"
		case "PING":
			return "+PONG\r\n"
		case "INFO":
			return respBulk("# Server\r\nserver_name:valkey\r\nvalkey_version:8.1.3\r\nredis_version:7.2.4\r\n")
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})
	runtime.Target.Config["server_family"] = ServerFamilyValkey
	runtime.Target.Config["database"] = 2
	runtime.Profile.Public = map[string]any{"username": "app"}
	runtime.Secrets = testSecrets{"password": "secret"}

	result, err := Connector{}.TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if result.Status != connectors.TestOK {
		t.Fatalf("result = %#v", result)
	}
	want := [][]string{
		{"AUTH", "app", "secret"},
		{"SELECT", "2"},
		{"PING"},
		{"INFO", "server"},
	}
	commandsMu.Lock()
	defer commandsMu.Unlock()
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestInfoActionReturnsValkeyIdentity(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		if !reflect.DeepEqual(command, []string{"INFO", "server"}) {
			t.Fatalf("command = %#v", command)
		}
		return respBulk("# Server\r\nserver_name:valkey\r\nvalkey_version:8.1.3\r\nredis_version:7.2.4\r\n")
	})
	result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
		ActionName: ActionInfo,
		Payload:    map[string]any{"section": "server"},
	})
	if err != nil {
		t.Fatalf("execute info: %v", err)
	}
	output := result.Output.(map[string]any)
	server := output["server"].(map[string]any)
	if server["detected_server_family"] != ServerFamilyValkey || server["server_version"] != "8.1.3" {
		t.Fatalf("server = %#v", server)
	}
	info := output["info"].(map[string]any)
	serverSection := info["Server"].(map[string]string)
	if serverSection["server_name"] != "valkey" || serverSection["valkey_version"] != "8.1.3" {
		t.Fatalf("info = %#v", info)
	}
	if !strings.Contains(result.DisplayText, "server_name:valkey") {
		t.Fatalf("display text = %q", result.DisplayText)
	}
}

func TestValkeyCompatibleKeyActions(t *testing.T) {
	t.Run("read string", func(t *testing.T) {
		runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
			switch command[0] {
			case "TYPE":
				return "+string\r\n"
			case "PTTL":
				return ":60000\r\n"
			case "GET":
				return respBulk(`{"ok":true}`)
			default:
				t.Fatalf("unexpected command = %#v", command)
				return ""
			}
		})
		result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
			ActionName: ActionGetKey,
			Payload:    map[string]any{"key": "app:status", "limit": 10, "max_bytes": 1024},
		})
		if err != nil {
			t.Fatalf("get key: %v", err)
		}
		output := result.Output.(map[string]any)
		if output["type"] != "string" || output["value"] != `{"ok":true}` || output["ttl_ms"] != int64(60000) {
			t.Fatalf("output = %#v", output)
		}
	})

	t.Run("set string", func(t *testing.T) {
		runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
			want := []string{"SET", "app:status", "ready", "EX", "60"}
			if !reflect.DeepEqual(command, want) {
				t.Fatalf("command = %#v, want %#v", command, want)
			}
			return "+OK\r\n"
		})
		result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
			ActionName: ActionSetString,
			Payload:    map[string]any{"key": "app:status", "value": "ready", "ttl_seconds": 60},
		})
		if err != nil {
			t.Fatalf("set string: %v", err)
		}
		if result.Status != connectors.ResultCompleted {
			t.Fatalf("result = %#v", result)
		}
	})

	t.Run("update ttl", func(t *testing.T) {
		runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
			want := []string{"EXPIRE", "app:status", "60"}
			if !reflect.DeepEqual(command, want) {
				t.Fatalf("command = %#v, want %#v", command, want)
			}
			return ":1\r\n"
		})
		result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
			ActionName: ActionExpireKey,
			Payload:    map[string]any{"key": "app:status", "ttl_seconds": 60},
		})
		if err != nil {
			t.Fatalf("expire key: %v", err)
		}
		if result.Output.(map[string]any)["changed"] != true {
			t.Fatalf("result = %#v", result)
		}
	})

	t.Run("delete keys", func(t *testing.T) {
		runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
			want := []string{"DEL", "app:one", "app:two"}
			if !reflect.DeepEqual(command, want) {
				t.Fatalf("command = %#v, want %#v", command, want)
			}
			return ":2\r\n"
		})
		result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
			ActionName: ActionDeleteKeys,
			Payload:    map[string]any{"keys": []any{"app:one", "app:two"}},
		})
		if err != nil {
			t.Fatalf("delete keys: %v", err)
		}
		if result.Output.(map[string]any)["deleted"] != int64(2) {
			t.Fatalf("result = %#v", result)
		}
	})
}

func testRuntimeWithScript(t *testing.T, handler func(*testing.T, []string) string) connectors.RuntimeContext {
	t.Helper()
	return connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID:            1,
			Ref:           "redis:1:2",
			ConnectorKind: Kind,
			Name:          "cache",
			Config:        map[string]any{"connection_mode": "direct", "host": "127.0.0.1", "port": 6379, "database": 0},
		},
		Profile:      connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "username_password", Label: "default"},
		Secrets:      testSecrets{},
		Capabilities: testCapabilities{transport: scriptedTransport{t: t, handler: handler}},
	}
}

type testSecrets map[string]string

func (secrets testSecrets) GetSecret(_ context.Context, name string) (string, error) {
	if value, ok := secrets[name]; ok {
		return value, nil
	}
	return "", connectors.ErrSecretNotFound
}

type failingSecrets struct{ err error }

func (secrets failingSecrets) GetSecret(context.Context, string) (string, error) {
	return "", secrets.err
}

type testCapabilities struct {
	transport connectors.NetworkTransport
}

func (capabilities testCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return capabilities.transport
	}
	return nil
}

type scriptedTransport struct {
	t       *testing.T
	handler func(*testing.T, []string) string
}

func (scriptedTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (transport scriptedTransport) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		reader := bufio.NewReader(server)
		for {
			value, err := readRESPValue(reader)
			if err != nil {
				return
			}
			command := respStringSlice(value)
			response := transport.handler(transport.t, command)
			if _, err := server.Write([]byte(response)); err != nil {
				return
			}
		}
	}()
	return client, nil
}

func respBulk(value string) string {
	return fmt.Sprintf("$%d\r\n%s\r\n", len(value), value)
}
