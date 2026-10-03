package conformance_test

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	rabbitmqconnector "github.com/aipermission/aipermission/backend/internal/connectors/rabbitmq"
)

func TestRabbitMQServerSearchBeyondInitialWindowRealService(t *testing.T) {
	requireConformance(t)
	runtime := rabbitFixtureRuntime(t)
	connector := rabbitmqconnector.New()
	assertConnection(t, connector, runtime)
	vhost := "aipermission-search-" + rand.Text()
	path := "/api/vhosts/" + url.PathEscape(vhost)
	t.Cleanup(func() { rabbitFixtureRequest(t, runtime, http.MethodDelete, path, nil) })
	rabbitFixtureRequest(t, runtime, http.MethodPut, path, map[string]any{})
	rabbitFixtureRequest(t, runtime, http.MethodPut, "/api/permissions/"+url.PathEscape(vhost)+"/"+url.PathEscape(runtime.Profile.Public["username"].(string)), map[string]any{
		"configure": ".*", "write": ".*", "read": ".*",
	})
	runtime.Target.Config["vhost"] = vhost
	const lastQueue = "jobs.zzz-final"
	for index := range 251 {
		name := fmt.Sprintf("jobs.%03d", index)
		if index == 250 {
			name = lastQueue
		}
		rabbitFixtureRequest(t, runtime, http.MethodPut, "/api/queues/"+url.PathEscape(vhost)+"/"+url.PathEscape(name), map[string]any{
			"durable": true, "auto_delete": false, "arguments": map[string]any{},
		})
		if t.Failed() {
			t.FailNow()
		}
	}
	initial := executeAction(t, connector, runtime, rabbitmqconnector.ActionListQueues, map[string]any{"limit": 250}).Output.(map[string]any)
	rows := initial["queues"].([]map[string]any)
	if len(rows) != 250 || initial["truncated"] != true || initial["scan_limit_reached"] != false {
		t.Fatalf("initial window did not report its bounded limit: %#v", initial)
	}
	for _, row := range rows {
		if row["name"] == lastQueue {
			t.Fatal("fixture's last queue unexpectedly appears in the initial sorted window")
		}
	}
	searched := executeAction(t, connector, runtime, rabbitmqconnector.ActionListQueues, map[string]any{"pattern": "ZZZ-FINAL", "limit": 250}).Output.(map[string]any)
	rows = searched["queues"].([]map[string]any)
	if len(rows) != 1 || rows[0]["name"] != lastQueue || searched["truncated"] != false || searched["scan_limit_reached"] != false {
		t.Fatalf("server-filtered queue outside initial window missing: %#v", searched)
	}
	detail := executeAction(t, connector, runtime, rabbitmqconnector.ActionGetQueue, map[string]any{"queue": lastQueue}).Output.(map[string]any)
	if detail["name"] != lastQueue || detail["vhost"] != vhost {
		t.Fatalf("searched queue selection identity changed: %#v", detail)
	}
}
