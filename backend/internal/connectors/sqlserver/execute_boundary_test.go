package sqlserverconnector

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type executionSecretSpy struct{ calls int }

func (s *executionSecretSpy) GetSecret(context.Context, string) (string, error) {
	s.calls++
	return "fixture-only", nil
}

func TestExecutionBoundaryRejectsDriftBeforeSecretResolution(t *testing.T) {
	for name, mutate := range map[string]func(*connectors.RuntimeContext, *connectors.PreparedAction){
		"action connector": func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.ConnectorKind = "other" },
		"action profile":   func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.ProfileID++ },
		"action target":    func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.TargetRef = "sqlserver:9:2" },
		"unknown action":   func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.ActionName = "other" },
		"read risk":        func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.Risk = connectors.RiskRead },
		"runtime target":   func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Target.Ref = "sqlserver:9:2" },
		"runtime profile":  func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Profile.TargetID++ },
		"runtime kind":     func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Profile.ConnectorKind = "other" },
		"runtime config":   func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Target.Config["port"] = 0 },
		"SQL changed":      func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.Payload["sql"] = "SELECT 1;\nGO 2" },
		"limits changed":   func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.Payload["timeout_seconds"] = 301 },
		"secret payload":   func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.Payload["password"] = "forbidden" },
		"read relabel":     func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.ActionName = ActionQueryReadonly },
		"write risk on read": func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) {
			a.ActionName, a.Payload = ActionQueryReadonly, map[string]any{"sql": "SELECT 1"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			runtime := fixtureRuntime()
			secret := &executionSecretSpy{}
			runtime.Secrets = secret
			action, err := New().PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionExecuteSQL, Input: map[string]any{"sql": "UPDATE dbo.t SET id=1;"}})
			if err != nil {
				t.Fatal(err)
			}
			mutate(&runtime, &action)
			if _, err := New().ExecuteAction(t.Context(), runtime, action); err == nil || secret.calls != 0 {
				t.Fatalf("drift delivered a secret: calls=%d err=%v", secret.calls, err)
			}
		})
	}
}

func TestExecutionActionAuthenticationFailureIsNotUnknown(t *testing.T) {
	runtime := fixtureRuntime()
	transport := &testTransport{err: errors.New("owned offline auth fixture")}
	runtime.Capabilities = transport
	action, err := New().PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionExecuteSQL, Input: map[string]any{"sql": "EXEC dbo.p;", "timeout_seconds": 120}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := New().ExecuteAction(t.Context(), runtime, action)
	if err == nil || result.Status == connectors.ResultOutcomeUnknown || transport.calls != 1 {
		t.Fatalf("authentication failure was retried or misclassified: %#v %v calls=%d", result, err, transport.calls)
	}
}

func TestExecutionHelpScopesReadRestrictionsAndBroadAuthority(t *testing.T) {
	help, err := New().GetHelp(t.Context(), fixtureRuntime().Target)
	all := strings.Join(help.Warnings, " ")
	if err != nil || !strings.Contains(all, "query_readonly rejects writes") || !strings.Contains(all, "not a current-database sandbox") || !strings.Contains(all, "permissions, routines and external effects") {
		t.Fatalf("unsafe help: %#v %v", help, err)
	}
}
