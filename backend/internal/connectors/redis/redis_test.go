package redisconnector

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestPrepareActionNormalizesRedisScan(t *testing.T) {
	target := connectors.TargetView{ID: 1, Ref: "redis:1:2", ConnectorKind: Kind, Name: "cache", Config: map[string]any{"connection_mode": "direct"}}
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

type testSecrets struct{}

func (testSecrets) GetSecret(context.Context, string) (string, error) {
	return "", fmt.Errorf("missing")
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
