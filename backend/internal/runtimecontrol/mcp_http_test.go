package runtimecontrol

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPHTTPHandlersCoordinateStartAndStopEffects(t *testing.T) {
	state := &State{}
	stopEffects := 0
	observed := make([]string, 0, 2)
	clock := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	handlers := NewMCPHTTPHandlers(func(http.ResponseWriter) (MCPRuntimeScope, bool) {
		return MCPRuntimeScope{
			State:        state,
			StartEnabled: func(context.Context) (bool, error) { return true, nil },
			AcquireStop:  func(context.Context) (func(), error) { return func() {}, nil },
			StopEffects:  func(context.Context) error { stopEffects++; return nil },
			Observe:      func(_ context.Context, action string, _ map[string]any) { observed = append(observed, action) },
			Now:          func() time.Time { return clock },
		}, true
	})

	started := httptest.NewRecorder()
	handlers.Update(started, newMCPUpdateRequest(`{"enabled":true}`))
	if started.Code != http.StatusOK || !state.MCPStarted() || stopEffects != 0 || observed[0] != "mcp.runtime.started" {
		t.Fatalf("start response=%d %s started=%v effects=%d observed=%v", started.Code, started.Body.String(), state.MCPStarted(), stopEffects, observed)
	}
	stopped := httptest.NewRecorder()
	handlers.Update(stopped, newMCPUpdateRequest(`{"enabled":false}`))
	if stopped.Code != http.StatusOK || state.MCPStarted() || stopEffects != 1 || observed[1] != "mcp.runtime.stopped" {
		t.Fatalf("stop response=%d %s started=%v effects=%d observed=%v", stopped.Code, stopped.Body.String(), state.MCPStarted(), stopEffects, observed)
	}
	if !strings.Contains(stopped.Body.String(), `"updated_at":"2026-09-10T08:30:00Z"`) {
		t.Fatalf("deterministic response = %s", stopped.Body.String())
	}
}

func TestMCPHTTPHandlersFailClosedWhenStopGateOrEffectsFail(t *testing.T) {
	state := &State{}
	state.SetMCPStarted(true)
	handlers := NewMCPHTTPHandlers(func(http.ResponseWriter) (MCPRuntimeScope, bool) {
		return MCPRuntimeScope{
			State:        state,
			StartEnabled: func(context.Context) (bool, error) { return false, nil },
			AcquireStop:  func(context.Context) (func(), error) { return nil, errors.New("gate canceled") },
			StopEffects:  func(context.Context) error { return nil },
			Observe:      func(context.Context, string, map[string]any) {},
		}, true
	})
	response := httptest.NewRecorder()
	handlers.Update(response, newMCPUpdateRequest(`{"enabled":false}`))
	if response.Code != http.StatusRequestTimeout || !state.MCPStarted() {
		t.Fatalf("gate failure response=%d %s started=%v", response.Code, response.Body.String(), state.MCPStarted())
	}

	handlers = NewMCPHTTPHandlers(func(http.ResponseWriter) (MCPRuntimeScope, bool) {
		return MCPRuntimeScope{
			State:        state,
			StartEnabled: func(context.Context) (bool, error) { return false, nil },
			AcquireStop:  func(context.Context) (func(), error) { return func() {}, nil },
			StopEffects:  func(context.Context) error { return errors.New("invalidate failed") },
			Observe:      func(context.Context, string, map[string]any) {},
		}, true
	})
	response = httptest.NewRecorder()
	handlers.Update(response, newMCPUpdateRequest(`{"enabled":false}`))
	if response.Code != http.StatusInternalServerError || state.MCPStarted() {
		t.Fatalf("effects failure response=%d %s started=%v", response.Code, response.Body.String(), state.MCPStarted())
	}
}

func newMCPUpdateRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPut, "/runtime", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
