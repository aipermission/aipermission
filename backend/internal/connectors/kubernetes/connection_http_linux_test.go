package kubernetesconnector

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestConnectionActualKubectlHTTP(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   int
		body   string
		status connectors.TestStatus
	}{
		{name: "empty", code: http.StatusOK, body: `{"kind":"PodList","apiVersion":"v1","items":[]}`, status: connectors.TestOK},
		{name: "populated first page", code: http.StatusOK, body: `{"kind":"PodList","apiVersion":"v1","metadata":{"continue":"next-page"},"items":[{"metadata":{"name":"fixture"}}]}`, status: connectors.TestOK},
		{name: "forbidden", code: http.StatusForbidden, body: `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"fixture access denied","code":403}`, status: connectors.TestFailedPermission},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, transport := ownedKubectl(t)
			requests := make(chan string, 8)
			server := ownedProbeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case requests <- r.Method + " " + r.URL.RequestURI():
				default:
					t.Error("unexpected extra probe requests")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.code)
				_, _ = w.Write([]byte(test.body))
			}))
			writeProbeKubeconfig(t, transport, server.URL)
			target.Config["context"] = "owned-context"
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err := New().TestConnection(ctx, connectors.RuntimeContext{
				Target: target, Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
			})
			if err != nil || result.Status != test.status {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if len(requests) != 1 {
				t.Fatalf("requests=%d, want one page without discovery or pagination", len(requests))
			}
			if got := <-requests; got != "GET /api/v1/namespaces/production/pods?limit=1" {
				t.Fatalf("probe request=%q", got)
			}
			if len(transport.requests) != 1 || transport.requests[0].TimeoutSeconds != 20 || transport.requests[0].TransportTargetRef != "ssh:2:20" {
				t.Fatalf("transport boundary=%#v", transport.requests)
			}
		})
	}
}

func ownedProbeServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("kubectl HTTP fixture requires loopback listen: %v", err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func writeProbeKubeconfig(t *testing.T, transport *cliProbeTransport, server string) {
	t.Helper()
	config := map[string]any{
		"apiVersion": "v1", "kind": "Config", "current-context": "owned-context",
		"clusters": []any{map[string]any{"name": "owned", "cluster": map[string]any{"server": server}}},
		"contexts": []any{map[string]any{"name": "owned-context", "context": map[string]any{"cluster": "owned"}}},
		"users":    []any{},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range transport.env {
		if path, ok := strings.CutPrefix(entry, "KUBECONFIG="); ok {
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("owned kubeconfig path missing")
}
