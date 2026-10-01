package connectorcredentials

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestCleanupProjectionPreservesErrorsAndRejectsUnavailableContext(t *testing.T) {
	failure := errors.New("known-secret")
	if err := RequireCompletedCleanup(connectors.ActionResult{Status: connectors.ResultCompleted}, failure); !errors.Is(err, failure) {
		t.Fatal("cleanup protocol error was overwritten by status")
	}
	ports := RuntimePorts{
		DecryptSecret: func(context.Context, int64, string) (map[string]any, error) { return nil, nil },
		RuntimeContext: func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, actionresult.CredentialBoundary) connectors.RuntimeContext {
			return connectors.RuntimeContext{}
		},
		RedactResult: func(context.Context, connectors.ActionResult, actionresult.CredentialBoundary) (connectors.ActionResult, error) {
			t.Fatal("unavailable/failed cleanup projected a result")
			return connectors.ActionResult{}, nil
		},
		RedactText: func(_ context.Context, value string) string { return value },
	}
	boundary := actionresult.NewCredentialBoundary(map[string]any{"password": "known-secret"})
	if result, err := ports.ProjectCompletedCleanup(t.Context(), connectors.ActionResult{Status: connectors.ResultCompleted}, failure, boundary); err == nil || strings.Contains(err.Error(), "known-secret") || result.Status != "" {
		t.Fatalf("failed cleanup leaked/accepted: %#v %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, input := range []context.Context{nil, ctx} {
		if result, err := ports.ProjectCompletedCleanup(input, connectors.ActionResult{Status: connectors.ResultCompleted}, nil, boundary); err == nil || result.Status != "" {
			t.Fatalf("unavailable cleanup projected: %#v %v", result, err)
		}
	}
	if result, err := (RuntimePorts{}).ProjectCompletedCleanup(t.Context(), connectors.ActionResult{Status: connectors.ResultCompleted}, nil, boundary); err == nil || result.Status != "" {
		t.Fatal("missing cleanup projector accepted")
	}
}

func TestCleanupProjectionRejectsAmbiguousResultsBeforeAndAfterProjection(t *testing.T) {
	for _, ambiguous := range []connectors.ActionResult{
		{Status: connectors.ResultCompleted, Error: "unconfirmed-secret"},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{RequestID: 4}},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{SessionID: 4}},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{SessionGeneration: 4}},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{BatchID: 4}},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{FollowupTool: "poll"}},
	} {
		for _, afterProjection := range []bool{false, true} {
			calls := 0
			ports := RuntimePorts{
				DecryptSecret: func(context.Context, int64, string) (map[string]any, error) {
					t.Fatal("cleanup projection must not decrypt")
					return nil, nil
				},
				RuntimeContext: func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, actionresult.CredentialBoundary) connectors.RuntimeContext {
					t.Fatal("cleanup projection must not construct runtime")
					return connectors.RuntimeContext{}
				},
				RedactResult: func(context.Context, connectors.ActionResult, actionresult.CredentialBoundary) (connectors.ActionResult, error) {
					calls++
					return ambiguous, nil
				},
				RedactText: func(_ context.Context, text string) string { return text },
			}
			input := ambiguous
			wantCalls := 0
			if afterProjection {
				input = connectors.ActionResult{Status: connectors.ResultCompleted}
				wantCalls = 1
			}
			result, err := ports.ProjectCompletedCleanup(t.Context(), input, nil, actionresult.NewCredentialBoundary(nil))
			if err == nil || !reflect.DeepEqual(result, connectors.ActionResult{}) || strings.Contains(err.Error(), "unconfirmed-secret") || calls != wantCalls {
				t.Fatalf("ambiguous cleanup accepted/leaked: after=%v result=%#v err=%v calls=%d", afterProjection, result, err, calls)
			}
		}
	}
}
