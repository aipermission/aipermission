package rabbitmqconnector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestListBindingsAcceptsBoundedArrayWithoutRefetch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  []map[string]any
		count int
	}{
		{"empty", []map[string]any{}, 0},
		{"exact queue", []map[string]any{
			{"destination": "jobs", "destination_type": "queue"},
			{"destination": " jobs ", "destination_type": "exchange"},
			{"destination": " jobs ", "destination_type": "queue", "routing_key": "exact"},
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.EscapedPath() != "/api/bindings/%2F" || r.URL.Query().Get("page") != "1" {
					t.Errorf("unexpected binding request: %s", r.URL)
				}
				_ = json.NewEncoder(w).Encode(tc.rows)
			}))
			defer server.Close()
			result, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
				ActionName: ActionListBindings,
				Payload:    map[string]any{"vhost": "/", "queue": " jobs ", "limit": 2},
			})
			if err != nil {
				t.Fatal(err)
			}
			output := result.Output.(map[string]any)
			if requests != 1 || output["count"] != tc.count || output["truncated"] != false || output["scan_limit_reached"] != false {
				t.Fatalf("requests=%d output=%#v", requests, output)
			}
		})
	}
}

func TestListBindingsArrayReportsOutputAndScanLimits(t *testing.T) {
	for _, tc := range []struct {
		name        string
		queue       string
		rows        int
		matching    bool
		count       int
		scanLimited bool
	}{
		{"unfiltered output", "", 4, true, 2, false},
		{"filtered output", " jobs ", 3, true, 2, false},
		{"exact scan boundary", " jobs ", maxRabbitListPageSize * maxBindingScanPages, false, 0, false},
		{"scan exhausted", " jobs ", maxRabbitListPageSize*maxBindingScanPages + 1, false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]map[string]any, tc.rows)
			for i := range rows {
				name := "other"
				if tc.matching {
					name = " jobs "
				}
				rows[i] = map[string]any{"destination": name, "destination_type": "queue"}
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				_ = json.NewEncoder(w).Encode(rows)
			}))
			defer server.Close()
			result, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
				ActionName: ActionListBindings,
				Payload:    map[string]any{"vhost": "/", "queue": tc.queue, "limit": 2},
			})
			if err != nil {
				t.Fatal(err)
			}
			output := result.Output.(map[string]any)
			if requests != 1 || output["count"] != tc.count || output["scan_limit_reached"] != tc.scanLimited || output["truncated"] != (tc.count == 2 || tc.scanLimited) {
				t.Fatalf("requests=%d output=%#v", requests, output)
			}
		})
	}
}

func TestListBindingsArrayRejectsInvalidAndOversizedResponses(t *testing.T) {
	for _, body := range []string{
		`[null]`, `[1]`, `[[]]`, `[{}]garbage`, `[{"destination":"\ud800"}]`,
		`[{"destination":"` + strings.Repeat("x", maxRabbitHTTPBodyBytes) + `"}]`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
			ActionName: ActionListBindings, Payload: map[string]any{"vhost": "/", "limit": 2},
		})
		server.Close()
		if err == nil {
			t.Fatalf("accepted invalid response of %d bytes", len(body))
		}
	}
}

func TestListBindingsRejectsSwitchFromPagesToArray(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"page": 1, "page_count": 2, "page_size": 500, "filtered_count": 2,
				"items": []map[string]any{{"destination": "other", "destination_type": "queue"}},
			})
			return
		}
		_, _ = w.Write([]byte(`[{"destination":"jobs","destination_type":"queue"}]`))
	}))
	defer server.Close()
	_, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
		ActionName: ActionListBindings, Payload: map[string]any{"vhost": "/", "queue": "jobs", "limit": 2},
	})
	if err == nil || requests != 2 {
		t.Fatalf("requests=%d error=%v", requests, err)
	}
}
