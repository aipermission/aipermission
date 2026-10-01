package rabbitmqconnector

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestRabbitResourceIdentitiesPreserveSchemaPreparedJSONAndWire(t *testing.T) {
	for _, identity := range []string{" jobs ", " \t ", "a/b?%#\u65e5", "\ufffd", "e\u0301"} {
		for _, name := range []string{ActionListQueues, ActionGetQueue, ActionListBindings, ActionPeekMessages, ActionPublish} {
			t.Run(fmt.Sprintf("%s/%q", name, identity), func(t *testing.T) {
				assertRabbitIdentityAction(t, name, identity)
			})
		}
	}
}

func assertRabbitIdentityAction(t *testing.T, name, identity string) {
	t.Helper()
	const vhost = " project /?%# "
	input := map[string]any{"vhost": vhost}
	if name == ActionPublish {
		input["exchange"], input["routing_key"], input["payload"] = identity, identity, "{}"
	} else if name != ActionListQueues {
		input["queue"] = identity
	}
	runtime := rabbitIdentityRuntime(t, func(w http.ResponseWriter, request *http.Request) {
		prefix := "/api/queues/" + url.PathEscape(vhost)
		switch name {
		case ActionListQueues:
			assertRabbitRequestPath(t, request, prefix)
			_ = json.NewEncoder(w).Encode(rabbitIdentityPage(t, request, []map[string]any{{"name": identity}, {"name": "sibling"}}))
		case ActionGetQueue:
			assertRabbitRequestPath(t, request, prefix+"/"+url.PathEscape(identity))
			_ = json.NewEncoder(w).Encode(map[string]any{"name": identity, "vhost": vhost})
		case ActionListBindings:
			assertRabbitRequestPath(t, request, "/api/bindings/"+url.PathEscape(vhost))
			_ = json.NewEncoder(w).Encode(rabbitIdentityPage(t, request, []map[string]any{
				{"destination_type": "queue", "destination": identity},
				{"destination_type": "queue", "destination": "sibling"},
			}))
		case ActionPeekMessages:
			assertRabbitRequestPath(t, request, prefix+"/"+url.PathEscape(identity)+"/get")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case ActionPublish:
			assertRabbitRequestPath(t, request, "/api/exchanges/"+url.PathEscape(vhost)+"/"+url.PathEscape(identity)+"/publish")
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["routing_key"] != identity {
				t.Errorf("wire routing identity changed: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"routed": true})
		}
	})
	actions, err := (Connector{}).GetActionList(t.Context(), runtime.Target, runtime.Profile)
	if err != nil {
		t.Fatal(err)
	}
	var definition connectors.ActionDefinition
	for _, action := range actions {
		if action.Name == name {
			definition = action
		}
	}
	normalized, err := connectors.NormalizeSchemaValues(definition.InputSchema, input)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{
		Target: runtime.Target, Profile: runtime.Profile, ActionName: name, Input: normalized,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"vhost", "queue", "exchange", "routing_key"} {
		if value, exists := input[field]; exists && (prepared.Payload[field] != value || prepared.Preview[field] != value) {
			t.Fatalf("%s changed before approval: payload=%#v preview=%#v", field, prepared.Payload, prepared.Preview)
		}
	}
	encoded, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	var decoded connectors.PreparedAction
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	result, err := (Connector{}).ExecuteAction(t.Context(), runtime, decoded)
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(map[string]any)
	if name == ActionGetQueue {
		if output["name"] != identity || output["vhost"] != vhost {
			t.Fatalf("read identity changed: %#v", output)
		}
	} else if output["vhost"] != vhost {
		t.Fatalf("result vhost changed: %#v", output)
	}
	if name == ActionListBindings && len(output["bindings"].([]map[string]any)) != 1 {
		t.Fatalf("binding filter lost the selected identity: %#v", output)
	}
	if name == ActionListQueues {
		queues := output["queues"].([]map[string]any)
		if len(queues) != 2 || queues[0]["name"] != identity || queues[1]["name"] != "sibling" {
			t.Fatalf("queue listing lost exact identities: %#v", output)
		}
	}
}

func TestRabbitVhostTargetAndEmptyDefaultsRemainExact(t *testing.T) {
	target := connectors.TargetView{Config: map[string]any{"vhost": " project "}}
	values, err := connectors.NormalizeSchemaValues((Connector{}).TargetSchema(), map[string]any{
		"vhost": " project ", "host": "127.0.0.1", "port": 15672, "connection_mode": "direct", "scheme": "http",
	})
	if err != nil || values["vhost"] != " project " {
		t.Fatalf("target vhost changed: %#v / %v", values, err)
	}
	for _, input := range []map[string]any{nil, {"vhost": "", "exchange": ""}} {
		input = copyMap(input)
		input["routing_key"], input["payload"] = "jobs", "{}"
		prepared, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{Target: target, ActionName: ActionPublish, Input: input})
		if err != nil || prepared.Payload["vhost"] != " project " || prepared.Payload["exchange"] != "amq.default" {
			t.Fatalf("missing/empty default changed: %#v / %v", prepared, err)
		}
	}
	for _, name := range []string{ActionGetQueue, ActionPeekMessages, ActionPublish} {
		input := map[string]any{"queue": "", "routing_key": "", "payload": "{}"}
		if _, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{ActionName: name, Input: input}); err == nil {
			t.Fatalf("%s accepted an exact empty identity", name)
		}
	}
}

func rabbitIdentityPage(t *testing.T, request *http.Request, rows []map[string]any) map[string]any {
	t.Helper()
	pageSize, err := strconv.Atoi(request.URL.Query().Get("page_size"))
	if err != nil || pageSize < len(rows) {
		t.Errorf("invalid requested page size: %q / %v", request.URL.RawQuery, err)
	}
	return map[string]any{"page": 1, "page_size": pageSize, "page_count": 1, "filtered_count": len(rows), "items": rows}
}

func assertRabbitRequestPath(t *testing.T, request *http.Request, expected string) {
	t.Helper()
	if request.URL.EscapedPath() != expected {
		t.Errorf("wire resource path = %q, want %q", request.URL.EscapedPath(), expected)
	}
}

func rabbitIdentityRuntime(t *testing.T, handler http.HandlerFunc) connectors.RuntimeContext {
	t.Helper()
	return connectors.RuntimeContext{
		Target: connectors.TargetView{Ref: "rabbitmq:1:2", ConnectorKind: Kind, Config: map[string]any{
			"connection_mode": "direct", "scheme": "http", "host": "127.0.0.1", "port": 15672, "vhost": "/",
		}},
		Profile:      connectors.CredentialProfileView{Public: map[string]any{"username": "guest"}},
		Secrets:      rabbitTestSecrets{"password": "secret"},
		Capabilities: rabbitTestCapabilities{transport: rabbitIdentityTransport{t: t, handler: handler}},
	}
}

type rabbitIdentityTransport struct {
	t       *testing.T
	handler http.HandlerFunc
}

func (rabbitIdentityTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (transport rabbitIdentityTransport) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	client, server := net.Pipe()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	_ = server.SetDeadline(time.Now().Add(5 * time.Second))
	transport.t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	go func() {
		defer server.Close()
		request, err := http.ReadRequest(bufio.NewReader(server))
		if err != nil {
			transport.t.Error(err)
			return
		}
		defer request.Body.Close()
		recorder := httptest.NewRecorder()
		transport.handler.ServeHTTP(recorder, request)
		response := recorder.Result()
		response.Close = true
		defer response.Body.Close()
		if err := response.Write(server); err != nil {
			transport.t.Error(err)
		}
	}()
	return client, nil
}

func TestRabbitListVhostNamesPreserveExactIdentity(t *testing.T) {
	want := []string{" / ", "/", " "}
	runtime := rabbitIdentityRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": want[0]}, {"name": want[1]}, {"name": want[2]}})
	})
	result, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionListVhosts})
	if err != nil || !reflect.DeepEqual(result.Output.(map[string]any)["names"], want) {
		t.Fatalf("vhost names changed: %#v / %v", result, err)
	}
}

func TestRabbitInvalidResourceIdentityRejectsBeforeTransport(t *testing.T) {
	for _, field := range []string{"vhost", "queue", "exchange", "routing_key"} {
		for _, invalid := range []any{42, true, []any{"job"}, "private-name:\xff"} {
			t.Run(fmt.Sprintf("%s/%T", field, invalid), func(t *testing.T) {
				input := map[string]any{"queue": "jobs", "routing_key": "jobs", "payload": "{}"}
				input[field] = invalid
				_, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{ActionName: ActionPublish, Input: input})
				if err == nil || !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), "private-name") {
					t.Fatalf("invalid identity prepared or echoed: %v", err)
				}
				_, err = (Connector{}).ExecuteAction(t.Context(), connectors.RuntimeContext{}, connectors.PreparedAction{ActionName: ActionPublish, Payload: input})
				if err == nil || !strings.Contains(err.Error(), field) {
					t.Fatalf("invalid identity reached runtime: %v", err)
				}
			})
		}
	}
}

func TestRabbitInvalidTargetIdentityRejectsBeforeTransport(t *testing.T) {
	for _, invalid := range []any{42, true, "private-name:\xff"} {
		runtime := connectors.RuntimeContext{Target: connectors.TargetView{Config: map[string]any{"vhost": invalid}}}
		_, err := newRabbitClient(t.Context(), runtime)
		if err == nil || !strings.Contains(err.Error(), "vhost") || strings.Contains(err.Error(), "private-name") {
			t.Fatalf("invalid target reached runtime or echoed identity: %v", err)
		}
		_, err = (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, ActionName: ActionOverview})
		if err == nil || !strings.Contains(err.Error(), "vhost") {
			t.Fatalf("invalid target prepared: %v", err)
		}
	}
}

func TestRabbitLossyUnicodeResponseRejectsWithoutRepair(t *testing.T) {
	for _, suffix := range []string{"\xff", `\ud800`, `\udfff`} {
		for _, action := range []string{ActionListVhosts, ActionPublish, ActionPeekMessages} {
			t.Run(fmt.Sprintf("%s/%q", action, suffix), func(t *testing.T) {
				runtime := rabbitIdentityRuntime(t, func(w http.ResponseWriter, r *http.Request) {
					body := `[{"name":"private-name:` + suffix + `"}]`
					if action == ActionPublish {
						body = `{"routed":true,"name":"private-name:` + suffix + `"}`
					}
					_, _ = w.Write([]byte(body))
				})
				prepared, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{
					Target: runtime.Target, ActionName: action,
					Input: map[string]any{"queue": "jobs", "routing_key": "jobs", "payload": "{}"},
				})
				if err != nil {
					t.Fatal(err)
				}
				result, err := (Connector{}).ExecuteAction(t.Context(), runtime, prepared)
				if err == nil || result.Output != nil || strings.Contains(err.Error(), "private-name") {
					t.Fatalf("lossy response repaired, published or echoed: %#v / %v", result, err)
				}
				if action != ActionListVhosts && connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
					t.Fatalf("dispatched mutation became safely retryable: %v", err)
				}
			})
		}
	}
}
