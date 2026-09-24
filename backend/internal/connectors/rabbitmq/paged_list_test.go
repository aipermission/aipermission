package rabbitmqconnector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestListQueuesRequestsBoundedServerFilteredPage(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/queues/%2F" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "3" || r.URL.Query().Get("name") != "job" {
			t.Errorf("unexpected queue request: %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page": 1, "page_size": 3, "page_count": 1, "filtered_count": 3,
			"items": []map[string]any{{"name": "job-a"}, {"name": "job-b"}, {"name": "job-c"}},
		})
	}))
	server.Start()
	defer server.Close()

	result, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
		ActionName: ActionListQueues,
		Payload:    map[string]any{"vhost": "/", "pattern": "job", "limit": 2},
	})
	if err != nil {
		t.Fatalf("list queues: %v", err)
	}
	output := result.Output.(map[string]any)
	if queues := output["queues"].([]map[string]any); len(queues) != 2 || queues[0]["name"] != "job-a" {
		t.Fatalf("queues = %#v", queues)
	}
	if output["truncated"] != true {
		t.Fatalf("truncated = %#v", output["truncated"])
	}
}

func TestListBindingsScansBoundedPagesForQueue(t *testing.T) {
	requests := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.EscapedPath() != "/api/bindings/%2F" {
			t.Errorf("unexpected binding path: %s", r.URL.String())
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 || page > 2 || r.URL.Query().Get("page_size") == "" {
			t.Errorf("unexpected binding pagination: %s", r.URL.RawQuery)
		}
		items := []map[string]any{{"destination": "other", "destination_type": "queue"}}
		if page == 2 {
			items = []map[string]any{{"destination": "jobs", "destination_type": "queue", "routing_key": "job.created"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page": page, "page_count": 2, "page_size": 500, "filtered_count": 2, "items": items,
		})
	}))
	server.Start()
	defer server.Close()

	result, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
		ActionName: ActionListBindings,
		Payload:    map[string]any{"vhost": "/", "queue": "jobs", "limit": 2},
	})
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	output := result.Output.(map[string]any)
	if bindings := output["bindings"].([]map[string]any); len(bindings) != 1 || bindings[0]["destination"] != "jobs" {
		t.Fatalf("bindings = %#v", bindings)
	}
	if requests != 2 || output["scan_limit_reached"] != false {
		t.Fatalf("requests=%d output=%#v", requests, output)
	}
}

func TestListBindingsMarksIncompleteQueueScan(t *testing.T) {
	requests := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page != requests || r.URL.Query().Get("page_size") != "500" {
			t.Errorf("unexpected binding request: %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page": page, "page_count": maxBindingScanPages + 1, "page_size": 500, "filtered_count": maxBindingScanPages + 1,
			"items": []map[string]any{{"destination": "other", "destination_type": "queue"}},
		})
	}))
	server.Start()
	defer server.Close()

	result, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
		ActionName: ActionListBindings,
		Payload:    map[string]any{"vhost": "/", "queue": "jobs", "limit": 2},
	})
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	output := result.Output.(map[string]any)
	if requests != maxBindingScanPages || output["scan_limit_reached"] != true || output["truncated"] != true || !strings.Contains(result.DisplayText, "more bindings may exist") {
		t.Fatalf("requests=%d output=%#v display=%q", requests, output, result.DisplayText)
	}
}

func TestListQueuesRejectsUnpaginatedResponse(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "jobs"}})
	}))
	server.Start()
	defer server.Close()

	_, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
		ActionName: ActionListQueues,
		Payload:    map[string]any{"vhost": "/", "limit": 2},
	})
	if err == nil {
		t.Fatal("expected unpaginated response to fail closed")
	}
}
