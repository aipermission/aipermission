package connectormanagement

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestHostPingLastAttemptCancellationIsNotACompletedMeasurement(t *testing.T) {
	for _, count := range []int{1, hostPingDefaultAttempts} {
		for _, success := range []bool{false, true} {
			t.Run(fmt.Sprintf("attempts=%d/success=%t", count, success), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				calls := 0
				handler, observed := cancellationPingHandler(func(context.Context, connectors.NetworkDialRequest) error {
					calls++
					if calls == count {
						cancel()
					}
					if success {
						return nil
					}
					return context.Canceled
				})
				response := cancellationPingRequest(handler, ctx, count)
				if calls != count || response.Code != http.StatusRequestTimeout || *observed || !strings.Contains(response.Body.String(), "ping canceled") {
					t.Fatalf("calls=%d status=%d observed=%v body=%s", calls, response.Code, *observed, response.Body.String())
				}
			})
		}
	}
}

func TestHostPingCanceledBeforeDispatchDoesNotProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, owner := range []context.Context{ctx, expired} {
		calls := 0
		handler, observed := cancellationPingHandler(func(context.Context, connectors.NetworkDialRequest) error { calls++; return nil })
		response := cancellationPingRequest(handler, owner, 1)
		if calls != 0 || response.Code != http.StatusRequestTimeout || *observed {
			t.Fatalf("calls=%d status=%d observed=%v body=%s", calls, response.Code, *observed, response.Body.String())
		}
	}
}

func TestHostPingProbeTimeoutIsStillACompletedFailedMeasurement(t *testing.T) {
	handler, observed := cancellationPingHandler(func(ctx context.Context, _ connectors.NetworkDialRequest) error {
		probe, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer cancel()
		return probe.Err()
	})
	response := cancellationPingRequest(handler, context.Background(), 1)
	var body HostPingResponse
	decodeManagementResponse(t, response, &body)
	if response.Code != http.StatusOK || !*observed || body.OK || body.Sent != 1 || body.Received != 0 || len(body.Attempts) != 1 || body.Attempts[0].Error != context.DeadlineExceeded.Error() {
		t.Fatalf("status=%d observed=%v body=%#v", response.Code, *observed, body)
	}
}

func TestHostPingCancellationAfterProbeDoesNotPublishResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scope := HostPingScope{
		Probe: func(context.Context, connectors.NetworkDialRequest) error { return context.DeadlineExceeded },
		Redact: func(_ context.Context, value string) string {
			cancel()
			return value
		},
	}
	response, completed := runHostPingAttempts(ctx, scope, HostPingRequest{Host: "localhost", Port: 22}, 1)
	if completed || response.Sent != 0 || len(response.Attempts) != 0 {
		t.Fatalf("canceled measurement published: completed=%v response=%#v", completed, response)
	}
}

func cancellationPingHandler(probe func(context.Context, connectors.NetworkDialRequest) error) (*HostPingHTTPHandler, *bool) {
	observed := false
	handler := NewHostPingHTTPHandler(func(http.ResponseWriter) (HostPingScope, bool) {
		return HostPingScope{
			ValidateTransport: func(context.Context, int64, string, string) error { return nil },
			Probe:             probe, Redact: func(_ context.Context, value string) string { return value },
			Observe: func(context.Context, string, map[string]any) { observed = true },
		}, true
	})
	return handler, &observed
}

func cancellationPingRequest(handler *HostPingHTTPHandler, ctx context.Context, count int) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(fmt.Sprintf(`{"host":"localhost","port":22,"attempts":%d}`, count))).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.Ping(response, request)
	return response
}
