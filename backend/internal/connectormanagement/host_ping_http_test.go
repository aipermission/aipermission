package connectormanagement

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestHostPingHTTPHandlerOwnsDirectAttemptAndObservationContract(t *testing.T) {
	dialCalls := 0
	validationCalls := 0
	var observation map[string]any
	handler := NewHostPingHTTPHandler(func(http.ResponseWriter) (HostPingScope, bool) {
		return HostPingScope{
			ValidateTransport: func(context.Context, int64, string, string) error {
				validationCalls++
				return nil
			},
			Probe: func(_ context.Context, request connectors.NetworkDialRequest) error {
				dialCalls++
				if request.Mode != "direct" || request.Host != "localhost" || request.Port != 5432 {
					t.Fatalf("dial request = %#v", request)
				}
				return nil
			},
			Redact: func(_ context.Context, value string) string { return value },
			Observe: func(_ context.Context, action string, payload map[string]any) {
				if action != "connector.host.ping" {
					t.Fatalf("observation action = %q", action)
				}
				observation = payload
			},
		}, true
	})
	response := performHostPingRequest(t, handler, HostPingRequest{
		Host: " localhost ", Port: 5432, Attempts: 1,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("ping status = %d: %s", response.Code, response.Body.String())
	}
	var body HostPingResponse
	decodeManagementResponse(t, response, &body)
	if !body.OK || body.Mode != "direct" || body.Sent != 1 || body.Received != 1 || body.Message != "Host and port are reachable." {
		t.Fatalf("ping response = %#v", body)
	}
	if dialCalls != 1 || validationCalls != 0 {
		t.Fatalf("dial=%d validate=%d", dialCalls, validationCalls)
	}
	if observation == nil || observation["received"] != 1 || observation["ok"] != true {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestHostPingHTTPHandlerValidatesConnectorTransportAndRedactsErrors(t *testing.T) {
	var validated struct {
		projectID int64
		mode      string
		targetRef string
	}
	handler := NewHostPingHTTPHandler(func(http.ResponseWriter) (HostPingScope, bool) {
		return HostPingScope{
			ValidateTransport: func(_ context.Context, projectID int64, mode, targetRef string) error {
				validated.projectID, validated.mode, validated.targetRef = projectID, mode, targetRef
				return nil
			},
			Probe: func(_ context.Context, request connectors.NetworkDialRequest) error {
				if request.SourceProjectID != 7 || request.TransportTargetRef != "ssh:2:3" {
					t.Fatalf("dial request = %#v", request)
				}
				return &net.DNSError{Name: "secret.internal", Err: "lookup failed"}
			},
			Redact: func(_ context.Context, value string) string {
				return strings.ReplaceAll(value, "secret.internal", "[REDACTED]")
			},
			Observe: func(context.Context, string, map[string]any) {},
		}, true
	})
	response := performHostPingRequest(t, handler, HostPingRequest{
		ProjectID: 7, Host: "db.internal", Port: 5432, Mode: " over_ssh ",
		TransportTargetRef: " ssh:2:3 ", Attempts: 1,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("ping status = %d: %s", response.Code, response.Body.String())
	}
	var body HostPingResponse
	decodeManagementResponse(t, response, &body)
	if body.OK || body.Received != 0 || len(body.Attempts) != 1 || body.Attempts[0].Error != "host lookup failed: [REDACTED]" {
		t.Fatalf("ping response = %#v", body)
	}
	if validated.projectID != 7 || validated.mode != "over_ssh" || validated.targetRef != "ssh:2:3" {
		t.Fatalf("validated = %#v", validated)
	}
}

func TestHostPingHTTPHandlerStopsBeforeProbeWhenTransportValidationFails(t *testing.T) {
	probed := false
	observed := false
	handler := NewHostPingHTTPHandler(func(http.ResponseWriter) (HostPingScope, bool) {
		return HostPingScope{
			ValidateTransport: func(context.Context, int64, string, string) error {
				return errors.New("transport is outside the project")
			},
			Probe: func(context.Context, connectors.NetworkDialRequest) error {
				probed = true
				return nil
			},
			Redact:  func(_ context.Context, value string) string { return value },
			Observe: func(context.Context, string, map[string]any) { observed = true },
		}, true
	})
	response := performHostPingRequest(t, handler, HostPingRequest{
		ProjectID: 7, Host: "db.internal", Port: 5432, Mode: "over_ssh",
		TransportTargetRef: "ssh:2:3", Attempts: 1,
	})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "outside the project") {
		t.Fatalf("validation response = %d: %s", response.Code, response.Body.String())
	}
	if probed || observed {
		t.Fatalf("probed=%v observed=%v", probed, observed)
	}
}

func TestHostPingHTTPHandlerFailsClosedAndRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		handler *HostPingHTTPHandler
		request HostPingRequest
		status  int
	}{
		{name: "nil provider", handler: NewHostPingHTTPHandler(nil), request: HostPingRequest{Host: "localhost", Port: 1}, status: http.StatusInternalServerError},
		{name: "missing capabilities", handler: NewHostPingHTTPHandler(func(http.ResponseWriter) (HostPingScope, bool) { return HostPingScope{}, true }), request: HostPingRequest{Host: "localhost", Port: 1}, status: http.StatusInternalServerError},
		{name: "missing host", handler: validHostPingHandler(), request: HostPingRequest{Port: 1}, status: http.StatusBadRequest},
		{name: "invalid port", handler: validHostPingHandler(), request: HostPingRequest{Host: "localhost", Port: 65536}, status: http.StatusBadRequest},
		{name: "missing project", handler: validHostPingHandler(), request: HostPingRequest{Host: "localhost", Port: 1, Mode: "over_ssh", TransportTargetRef: "ssh:1:1"}, status: http.StatusBadRequest},
		{name: "missing transport", handler: validHostPingHandler(), request: HostPingRequest{ProjectID: 1, Host: "localhost", Port: 1, Mode: "over_ssh"}, status: http.StatusBadRequest},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := performHostPingRequest(t, testCase.handler, testCase.request)
			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, testCase.status, response.Body.String())
			}
		})
	}
}

func TestHostPingHTTPHandlerRejectsCanceledSeriesWithoutObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observed := false
	handler := NewHostPingHTTPHandler(func(http.ResponseWriter) (HostPingScope, bool) {
		return HostPingScope{
			ValidateTransport: func(context.Context, int64, string, string) error { return nil },
			Probe: func(context.Context, connectors.NetworkDialRequest) error {
				cancel()
				return errors.New("offline")
			},
			Redact:  func(_ context.Context, value string) string { return value },
			Observe: func(context.Context, string, map[string]any) { observed = true },
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(`{"host":"localhost","port":22,"attempts":2}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.Ping(response, request)
	if response.Code != http.StatusRequestTimeout || observed {
		t.Fatalf("status=%d observed=%v body=%s", response.Code, observed, response.Body.String())
	}
}

func TestHostPingAttemptCountIsBoundedAtTheExecutionBoundary(t *testing.T) {
	for input, expected := range map[int]int{
		-1: hostPingDefaultAttempts,
		0:  hostPingDefaultAttempts,
		1:  1,
		4:  hostPingDefaultAttempts,
		5:  hostPingDefaultAttempts,
	} {
		if actual := boundedHostPingAttempts(input); actual != expected {
			t.Fatalf("boundedHostPingAttempts(%d) = %d, want %d", input, actual, expected)
		}
	}
}

func validHostPingHandler() *HostPingHTTPHandler {
	return NewHostPingHTTPHandler(func(http.ResponseWriter) (HostPingScope, bool) {
		return HostPingScope{
			ValidateTransport: func(context.Context, int64, string, string) error { return nil },
			Probe:             func(context.Context, connectors.NetworkDialRequest) error { return nil },
			Redact:            func(_ context.Context, value string) string { return value },
			Observe:           func(context.Context, string, map[string]any) {},
		}, true
	})
}

func performHostPingRequest(t *testing.T, handler *HostPingHTTPHandler, request HostPingRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(string(body)))
	httpRequest.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.Ping(response, httpRequest)
	return response
}
