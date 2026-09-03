package rabbitmqconnector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestPrepareActionNormalizesPeekMessages(t *testing.T) {
	target := connectors.TargetView{
		ID:            1,
		Ref:           "rabbitmq:1:2",
		ConnectorKind: Kind,
		Name:          "queue",
		Config:        map[string]any{"connection_mode": "direct", "scheme": "http", "host": "127.0.0.1", "port": 15672, "vhost": "/"},
	}
	profile := connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "username_password", Label: "monitor"}

	prepared, err := Connector{}.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     target,
		Profile:    profile,
		ActionName: ActionPeekMessages,
		Input:      map[string]any{"queue": "jobs", "count": 500, "max_payload_bytes": 1 << 30},
	})
	if err != nil {
		t.Fatalf("prepare peek: %v", err)
	}
	if prepared.Payload["count"] != maxPeekCount {
		t.Fatalf("count = %#v", prepared.Payload["count"])
	}
	if prepared.Payload["max_payload_bytes"] != maxPayloadBytes {
		t.Fatalf("max_payload_bytes = %#v", prepared.Payload["max_payload_bytes"])
	}
	if prepared.Payload["vhost"] != "/" {
		t.Fatalf("vhost = %#v", prepared.Payload["vhost"])
	}
	if prepared.Risk != connectors.RiskWrite {
		t.Fatalf("risk = %q", prepared.Risk)
	}
	if policy := connectors.EffectiveRetryPolicy(connectors.ActionDefinition{Risk: prepared.Risk}); policy.Class != connectors.RetryNonIdempotent {
		t.Fatalf("retry policy = %#v", policy)
	}
}

func TestExecuteActionListsQueuesThroughNetworkTransport(t *testing.T) {
	var seenAuth bool
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "guest" || password != "secret" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		seenAuth = true
		if r.URL.EscapedPath() != "/api/queues/%2F" {
			t.Fatalf("path = %q escaped=%q", r.URL.Path, r.URL.EscapedPath())
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "jobs", "vhost": "/", "messages_ready": 3, "messages_unacknowledged": 1, "messages": 4, "consumers": 2, "state": "running"},
			{"name": "events", "vhost": "/", "messages_ready": 0, "messages_unacknowledged": 0, "messages": 0, "consumers": 1, "state": "running"},
		})
	}))
	server.Start()
	defer server.Close()

	result, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
		ActionName: ActionListQueues,
		Payload:    map[string]any{"vhost": "/", "pattern": "job", "limit": 10},
	})
	if err != nil {
		t.Fatalf("execute list queues: %v", err)
	}
	if !seenAuth {
		t.Fatal("expected basic auth")
	}
	output := result.Output.(map[string]any)
	queues := output["queues"].([]map[string]any)
	if len(queues) != 1 || queues[0]["name"] != "jobs" {
		t.Fatalf("queues = %#v", queues)
	}
	if result.DisplayText == "" || !strings.Contains(result.DisplayText, "jobs") {
		t.Fatalf("display = %q", result.DisplayText)
	}
}

func TestExecuteActionPeeksMessagesWithRequeue(t *testing.T) {
	var body map[string]any
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/queues/%2F/jobs/get" {
			t.Fatalf("path = %q escaped=%q", r.URL.Path, r.URL.EscapedPath())
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"payload": `{"ok":true}`, "payload_encoding": "string", "redelivered": false},
		})
	}))
	server.Start()
	defer server.Close()

	result, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
		ActionName: ActionPeekMessages,
		Payload:    map[string]any{"vhost": "/", "queue": "jobs", "count": 2, "max_payload_bytes": 4096},
	})
	if err != nil {
		t.Fatalf("execute peek: %v", err)
	}
	if body["ackmode"] != "ack_requeue_true" || body["count"].(float64) != 2 {
		t.Fatalf("body = %#v", body)
	}
	output := result.Output.(map[string]any)
	if output["ackmode"] != "ack_requeue_true" || output["count"] != 1 {
		t.Fatalf("output = %#v", output)
	}
}

func TestPrepareActionNormalizesPublishMessage(t *testing.T) {
	target := connectors.TargetView{
		ID:            1,
		Ref:           "rabbitmq:1:2",
		ConnectorKind: Kind,
		Name:          "queue",
		Config:        map[string]any{"connection_mode": "direct", "scheme": "http", "host": "127.0.0.1", "port": 15672, "vhost": "/"},
	}
	profile := connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "username_password", Label: "writer"}

	prepared, err := Connector{}.PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     target,
		Profile:    profile,
		ActionName: ActionPublish,
		Input: map[string]any{
			"routing_key": "jobs",
			"payload":     `{"hello":"world"}`,
			"properties":  `{"content_type":"application/json"}`,
		},
	})
	if err != nil {
		t.Fatalf("prepare publish: %v", err)
	}
	if prepared.Risk != connectors.RiskWrite {
		t.Fatalf("risk = %q", prepared.Risk)
	}
	if prepared.Payload["exchange"] != "amq.default" || prepared.Payload["payload_encoding"] != "string" {
		t.Fatalf("payload = %#v", prepared.Payload)
	}
	properties := prepared.Payload["properties"].(map[string]any)
	if properties["content_type"] != "application/json" {
		t.Fatalf("properties = %#v", properties)
	}
}

func TestExecuteActionPublishesMessage(t *testing.T) {
	var body map[string]any
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/exchanges/%2F/amq.default/publish" {
			t.Fatalf("path = %q escaped=%q", r.URL.Path, r.URL.EscapedPath())
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"routed": true})
	}))
	server.Start()
	defer server.Close()

	result, err := Connector{}.ExecuteAction(context.Background(), testRuntimeForServer(t, server), connectors.PreparedAction{
		ActionName: ActionPublish,
		Payload: map[string]any{
			"vhost":            "/",
			"exchange":         "amq.default",
			"routing_key":      "jobs",
			"payload":          `{"ok":true}`,
			"payload_encoding": "string",
			"properties":       map[string]any{"content_type": "application/json"},
		},
	})
	if err != nil {
		t.Fatalf("execute publish: %v", err)
	}
	if body["routing_key"] != "jobs" || body["payload_encoding"] != "string" {
		t.Fatalf("body = %#v", body)
	}
	output := result.Output.(map[string]any)
	if output["routed"] != true || output["payload_bytes"] != len(`{"ok":true}`) {
		t.Fatalf("output = %#v", output)
	}
}

func TestConnectionReportsAuthFailure(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	server.Start()
	defer server.Close()

	result, err := Connector{}.TestConnection(context.Background(), testRuntimeForServer(t, server))
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if result.Status != connectors.TestFailedAuth {
		t.Fatalf("status = %#v message=%q", result.Status, result.Message)
	}
}

func TestConnectionDistinguishesMissingSecretFromProviderFailure(t *testing.T) {
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	runtime := testRuntimeForServer(t, server)
	runtime.Secrets = rabbitTestSecrets{}
	missing, err := Connector{}.TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("missing-secret connection test: %v", err)
	}
	if missing.Status != connectors.TestFailedAuth || !strings.Contains(missing.Message, ErrMissingSecret.Error()) {
		t.Fatalf("missing-secret result = %#v", missing)
	}

	providerErr := errors.New("vault unavailable")
	runtime.Secrets = rabbitFailingSecrets{err: providerErr}
	failed, err := Connector{}.TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("provider-failure connection test: %v", err)
	}
	if failed.Status != connectors.TestUnknownError || !strings.Contains(failed.Message, "resolve rabbitmq password") {
		t.Fatalf("provider-failure result = %#v", failed)
	}
}

func testRuntimeForServer(t *testing.T, server *httptest.Server) connectors.RuntimeContext {
	t.Helper()
	host, port := splitServerAddr(t, server.Listener.Addr().String())
	return connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID:            1,
			Ref:           "rabbitmq:1:2",
			ConnectorKind: Kind,
			Name:          "queue",
			Config:        map[string]any{"connection_mode": "direct", "scheme": "http", "host": host, "port": port, "vhost": "/"},
		},
		Profile: connectors.CredentialProfileView{
			ID:            2,
			TargetID:      1,
			ConnectorKind: Kind,
			Kind:          "username_password",
			Label:         "monitor",
			Public:        map[string]any{"username": "guest"},
		},
		Secrets:      rabbitTestSecrets{"password": "secret"},
		Capabilities: rabbitTestCapabilities{transport: rabbitHTTPTransport{targetAddr: server.Listener.Addr().String()}},
	}
}

func splitServerAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil {
		t.Fatalf("scan port: %v", err)
	}
	return host, port
}

type rabbitTestSecrets map[string]string

func (secrets rabbitTestSecrets) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := secrets[name]
	if !ok {
		return "", connectors.ErrSecretNotFound
	}
	return value, nil
}

type rabbitFailingSecrets struct{ err error }

func (secrets rabbitFailingSecrets) GetSecret(context.Context, string) (string, error) {
	return "", secrets.err
}

type rabbitTestCapabilities struct {
	transport connectors.NetworkTransport
}

func (capabilities rabbitTestCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return capabilities.transport
	}
	return nil
}

type rabbitHTTPTransport struct {
	targetAddr string
	requests   []connectors.NetworkDialRequest
}

func (rabbitHTTPTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (transport rabbitHTTPTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	if request.Mode != "direct" {
		return nil, fmt.Errorf("mode = %q", request.Mode)
	}
	if request.Host == "" || request.Port == 0 {
		return nil, fmt.Errorf("invalid dial request: %#v", request)
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", transport.targetAddr)
}

func TestActionListNames(t *testing.T) {
	actions, err := Connector{}.GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatalf("actions: %v", err)
	}
	names := make([]string, 0, len(actions))
	for _, action := range actions {
		names = append(names, action.Name)
	}
	want := []string{ActionOverview, ActionListVhosts, ActionListQueues, ActionGetQueue, ActionListBindings, ActionPeekMessages, ActionPublish}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %#v", names)
	}
}

func TestRabbitSchemePreservesSavedValuesAndSecuresNewRemoteTargets(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		want   string
	}{
		{name: "missing legacy value", config: map[string]any{"host": "rabbit.internal"}, want: "http"},
		{name: "explicit HTTP", config: map[string]any{"scheme": "http", "host": "rabbit.internal"}, want: "http"},
		{name: "explicit HTTPS over SSH", config: map[string]any{"scheme": "https", "connection_mode": "over_ssh", "host": "127.0.0.1"}, want: "https"},
		{name: "auto remote direct", config: map[string]any{"scheme": "auto", "connection_mode": "direct", "host": "rabbit.internal"}, want: "https"},
		{name: "auto local direct", config: map[string]any{"scheme": "auto", "connection_mode": "direct", "host": "127.0.0.1"}, want: "http"},
		{name: "auto over SSH", config: map[string]any{"scheme": "auto", "connection_mode": "over_ssh", "host": "rabbit.internal"}, want: "http"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := rabbitScheme(connectors.TargetView{Config: test.config}); got != test.want {
				t.Fatalf("rabbitScheme(%#v) = %q, want %q", test.config, got, test.want)
			}
		})
	}
}

func TestPublishMarksPayloadAndPropertiesAsSensitiveInput(t *testing.T) {
	actions, err := Connector{}.GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatalf("actions: %v", err)
	}
	for _, action := range actions {
		if action.Name == ActionPublish {
			if !reflect.DeepEqual(action.SensitiveInputFields, []string{"payload", "properties"}) {
				t.Fatalf("sensitive fields = %#v", action.SensitiveInputFields)
			}
			return
		}
	}
	t.Fatal("publish_message action was not found")
}

func TestRabbitMutationPostDispatchFailureIsOutcomeUnknown(t *testing.T) {
	err := classifyRabbitMutationError("publish message", &rabbitPostDispatchError{err: errors.New("unexpected EOF")})
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
		t.Fatalf("error = %v code=%q status=%q", err, connectors.ErrorCode(err), connectors.ErrorStatus(err))
	}
	definite := errors.New("permission denied")
	if classified := classifyRabbitMutationError("publish message", definite); classified != definite {
		t.Fatalf("definite response was reclassified: %v", classified)
	}
}

func TestRabbitPostTracksMutationDispatchAndAmbiguousStatuses(t *testing.T) {
	preDispatch := &rabbitClient{
		baseURL: "http://rabbit.invalid",
		httpClient: &http.Client{Transport: rabbitRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial refused")
		})},
	}
	if err := preDispatch.Post(t.Context(), "/publish", map[string]any{"payload": "x"}, nil); err == nil || connectors.ErrorStatus(classifyRabbitMutationError("publish message", err)) == connectors.ResultOutcomeUnknown {
		t.Fatalf("pre-dispatch error was misclassified: %v", err)
	}

	postDispatch := &rabbitClient{
		baseURL: "http://rabbit.invalid",
		httpClient: &http.Client{Transport: rabbitRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			trace := httptrace.ContextClientTrace(request.Context())
			trace.WroteRequest(httptrace.WroteRequestInfo{})
			return nil, errors.New("connection reset")
		})},
	}
	err := postDispatch.Post(t.Context(), "/publish", map[string]any{"payload": "x"}, nil)
	if connectors.ErrorStatus(classifyRabbitMutationError("publish message", err)) != connectors.ResultOutcomeUnknown {
		t.Fatalf("post-dispatch error was not classified as unknown: %v", err)
	}

	for _, status := range []int{http.StatusRequestTimeout, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "ambiguous", status)
			}))
			defer server.Close()
			client := &rabbitClient{baseURL: server.URL, httpClient: server.Client()}
			err := client.Post(t.Context(), "/publish", map[string]any{"payload": "x"}, nil)
			if connectors.ErrorStatus(classifyRabbitMutationError("publish message", err)) != connectors.ResultOutcomeUnknown {
				t.Fatalf("status %d was not classified as unknown: %v", status, err)
			}
		})
	}
}

type rabbitRoundTripFunc func(*http.Request) (*http.Response, error)

func (function rabbitRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
