package gatewayvault

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type mcpStopRecorder struct {
	calls  []string
	failAt string
}

func (recorder *mcpStopRecorder) InvalidateAll(_ context.Context, reason string) error {
	recorder.calls = append(recorder.calls, "invalidate:"+reason)
	return recorder.failure("invalidate")
}

func (recorder *mcpStopRecorder) StalePendingForAction(_ context.Context, action, reason string) error {
	recorder.calls = append(recorder.calls, "stale:"+action+":"+reason)
	return recorder.failure("stale")
}

func (recorder *mcpStopRecorder) FailRunning(_ context.Context, reason string) error {
	recorder.calls = append(recorder.calls, "fail:"+reason)
	return recorder.failure("fail")
}

func (recorder *mcpStopRecorder) failure(step string) error {
	if recorder.failAt == step {
		return errors.New(step)
	}
	return nil
}

func TestStopMCPOwnsVaultCleanupOrder(t *testing.T) {
	recorder := &mcpStopRecorder{}
	if err := New(Dependencies{}).StopMCP(t.Context(), recorder, recorder); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"invalidate:" + mcpStoppedRequestReason,
		"stale:" + ActionGenerateItem + ":" + mcpStoppedRequestReason,
		"fail:" + mcpStoppedRunningReason,
	}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("unexpected MCP stop effects: got %v want %v", recorder.calls, want)
	}
}

func TestStopMCPAttemptsEveryCleanupAfterFailures(t *testing.T) {
	for _, step := range []string{"invalidate", "stale", "fail"} {
		t.Run(step, func(t *testing.T) {
			recorder := &mcpStopRecorder{failAt: step}
			if err := New(Dependencies{}).StopMCP(t.Context(), recorder, recorder); err == nil || err.Error() != step {
				t.Fatalf("expected %s failure, got %v", step, err)
			}
			if len(recorder.calls) != 3 {
				t.Fatalf("cleanup stopped after %s failure: %v", step, recorder.calls)
			}
		})
	}
}

func TestStopMCPRejectsIncompleteComposition(t *testing.T) {
	recorder := &mcpStopRecorder{}
	for _, run := range []func() error{
		func() error { return (*Component)(nil).StopMCP(t.Context(), recorder, recorder) },
		func() error { return New(Dependencies{}).StopMCP(t.Context(), nil, recorder) },
		func() error { return New(Dependencies{}).StopMCP(t.Context(), recorder, nil) },
	} {
		if err := run(); !errors.Is(err, InvalidatorUnavailableError()) {
			t.Fatalf("expected fail-closed composition error, got %v", err)
		}
	}
}
