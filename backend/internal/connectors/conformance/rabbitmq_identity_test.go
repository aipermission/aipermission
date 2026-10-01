package conformance_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	rabbitmqconnector "github.com/aipermission/aipermission/backend/internal/connectors/rabbitmq"
)

func TestRabbitMQResourceIdentityRealService(t *testing.T) {
	requireConformance(t)
	runtime := rabbitFixtureRuntime(t)
	connector := rabbitmqconnector.New()
	assertConnection(t, connector, runtime)
	vhost := " aipermission-identity-" + rand.Text() + " "
	path := "/api/vhosts/" + url.PathEscape(vhost)
	t.Cleanup(func() { rabbitFixtureRequest(t, runtime, http.MethodDelete, path, nil) })
	rabbitFixtureRequest(t, runtime, http.MethodPut, path, map[string]any{})
	rabbitFixtureRequest(t, runtime, http.MethodPut, "/api/permissions/"+url.PathEscape(vhost)+"/"+url.PathEscape(runtime.Profile.Public["username"].(string)), map[string]any{
		"configure": ".*", "write": ".*", "read": ".*",
	})
	runtime.Target.Config["vhost"] = vhost
	const queue = " jobs "
	for _, name := range []string{queue, "jobs"} {
		rabbitFixtureRequest(t, runtime, http.MethodPut, "/api/queues/"+url.PathEscape(vhost)+"/"+url.PathEscape(name), map[string]any{
			"durable": true, "auto_delete": false, "arguments": map[string]any{},
		})
	}
	list := executeAction(t, connector, runtime, rabbitmqconnector.ActionListQueues, nil).Output.(map[string]any)
	names := map[string]bool{}
	for _, row := range list["queues"].([]map[string]any) {
		names[row["name"].(string)] = true
	}
	if list["vhost"] != vhost || len(names) != 2 || !names[queue] || !names["jobs"] {
		t.Fatalf("owned queue identities merged or trimmed: %#v", list)
	}
	for _, exchange := range []string{"amq.default", " exchange "} {
		if exchange != "amq.default" {
			fixtureExchange := "/api/exchanges/" + url.PathEscape(vhost) + "/" + url.PathEscape(exchange)
			rabbitFixtureRequest(t, runtime, http.MethodPut, fixtureExchange, map[string]any{
				"type": "direct", "durable": false, "auto_delete": false, "internal": false, "arguments": map[string]any{},
			})
			rabbitFixtureRequest(t, runtime, http.MethodPost, "/api/bindings/"+url.PathEscape(vhost)+"/e/"+url.PathEscape(exchange)+"/q/"+url.PathEscape(queue), map[string]any{
				"routing_key": queue, "arguments": map[string]any{},
			})
		}
		published := executeAction(t, connector, runtime, rabbitmqconnector.ActionPublish, map[string]any{
			"exchange": exchange, "routing_key": queue, "payload": "owned identity fixture",
		}).Output.(map[string]any)
		if published["routed"] != true || published["exchange"] != exchange || published["routing_key"] != queue || published["vhost"] != vhost {
			t.Fatalf("publish identity changed or did not route: %#v", published)
		}
	}
	peek := executeAction(t, connector, runtime, rabbitmqconnector.ActionPeekMessages, map[string]any{"queue": queue, "count": 2}).Output.(map[string]any)
	if peek["queue"] != queue || peek["vhost"] != vhost || len(peek["messages"].([]map[string]any)) != 2 {
		t.Fatalf("owned padded queue did not receive both messages: %#v", peek)
	}
	sibling := executeAction(t, connector, runtime, rabbitmqconnector.ActionPeekMessages, map[string]any{"queue": "jobs", "count": 2}).Output.(map[string]any)
	if len(sibling["messages"].([]map[string]any)) != 0 {
		t.Fatalf("publish reached the wrong queue: %#v", sibling)
	}
	metadata := executeAction(t, connector, runtime, rabbitmqconnector.ActionGetQueue, map[string]any{"queue": queue}).Output.(map[string]any)
	if metadata["name"] != queue || metadata["vhost"] != vhost {
		t.Fatalf("queue metadata identity changed: %#v", metadata)
	}
	bindings := executeAction(t, connector, runtime, rabbitmqconnector.ActionListBindings, map[string]any{"queue": queue}).Output.(map[string]any)
	rows := bindings["bindings"].([]map[string]any)
	if bindings["vhost"] != vhost || bindings["queue"] != queue || len(rows) < 1 {
		t.Fatalf("selected queue binding identity changed: %#v", bindings)
	}
	for _, row := range rows {
		if row["destination"] != queue || row["vhost"] != vhost {
			t.Fatalf("binding listing included the wrong resource: %#v", row)
		}
	}
}

func rabbitFixtureRequest(t *testing.T, runtime connectors.RuntimeContext, method, path string, body any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Error(err)
		return
	}
	host := runtime.Target.Config["host"].(string)
	port := runtime.Target.Config["port"].(int)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+path, bytes.NewReader(encoded))
	if err != nil {
		t.Error(err)
		return
	}
	request.SetBasicAuth(runtime.Profile.Public["username"].(string), runtime.Secrets.(fixtureSecrets)["password"])
	request.Header.Set("Content-Type", "application/json")
	client := connectors.NewHTTPClient(directNetworkTransport{}, connectors.NetworkDialRequest{Mode: "direct", Host: host, Port: port}, 5*time.Second)
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		t.Errorf("owned RabbitMQ fixture %s: %v", method, err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Error(fmt.Errorf("owned RabbitMQ fixture %s status: %d", method, response.StatusCode))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
}
