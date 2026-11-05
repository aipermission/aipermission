package postgresconnector

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
)

func TestExecuteSQLCatalogAndPreparation(t *testing.T) {
	runtime := cleanupFixtureRuntime(nil)
	runtime.Target.Config["connection_mode"] = "over_ssh"
	runtime.Target.Config["transport_target_ref"] = "ssh:3:4"
	catalog, err := New().GetActionList(t.Context(), runtime.Target, runtime.Profile)
	if err != nil {
		t.Fatal(err)
	}
	definition := catalog[len(catalog)-1]
	if !reflect.DeepEqual(definition, sqlexecute.Definition(Label)) || definition.Risk != connectors.RiskWrite || definition.RetryPolicy.Class != connectors.RetryNonIdempotent {
		t.Fatal("SQL action does not expose the shared write/non-idempotent contract")
	}
	encoded, err := json.Marshal(definition)
	if err != nil || strings.Contains(string(encoded), "password") {
		t.Fatal("SQL action schema must not contain database credentials")
	}
	help, err := New().GetHelp(t.Context(), runtime.Target)
	if err != nil || !strings.Contains(strings.Join(help.Warnings, "\n"), "server-granted authority") || !strings.Contains(strings.Join(help.Warnings, "\n"), "not confined to the configured database") {
		t.Fatal("SQL help must disclose broad server-granted authority and external effects")
	}
	for _, script := range []string{
		"\n  INSERT INTO public.items VALUES (1); UPDATE public.items SET id = 2; DELETE FROM public.items;\n",
		"BEGIN; CREATE TABLE public.items (id int); ALTER TABLE public.items ADD COLUMN value text; COMMIT;",
		"CREATE PROCEDURE public.p() LANGUAGE plpgsql AS $$BEGIN RAISE NOTICE 'a;b'; END$$; CALL public.p(); DO $body$BEGIN PERFORM 1; END$body$;",
	} {
		req := connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionExecuteSQL, Input: map[string]any{"sql": script}}
		connectortest.AssertPrepareActionDeterministic(t, New(), req)
		prepared := prepareExecuteFixture(t, runtime, req.ActionName, req.Input)
		if prepared.Payload["sql"] != script || prepared.Preview["sql"] != script || prepared.Risk != connectors.RiskWrite {
			t.Fatal("preparation changed the full approved script or risk")
		}
		if !reflect.DeepEqual(prepared.Dependencies, connectors.NetworkTransportDependencies(runtime.Target)) {
			t.Fatal("SQL preparation lost its SSH approval dependency")
		}
	}
}

func TestExecuteSQLRejectsInvalidInput(t *testing.T) {
	runtime := cleanupFixtureRuntime(nil)
	for name, input := range map[string]map[string]any{
		"empty": {"sql": " \n"}, "nul": {"sql": "SELECT 1\x00"}, "utf8": {"sql": string([]byte{0xff})},
		"large":     {"sql": strings.Repeat("x", sqlexecute.MaxSQLBytes+1)},
		"rows-zero": {"sql": "SELECT 1", "max_rows": 0}, "rows-large": {"sql": "SELECT 1", "max_rows": 1001},
		"fractional":   {"sql": "SELECT 1", "timeout_seconds": 1.5},
		"timeout-zero": {"sql": "SELECT 1", "timeout_seconds": 0}, "timeout-large": {"sql": "SELECT 1", "timeout_seconds": 301},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New().PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionExecuteSQL, Input: input})
			if err == nil {
				t.Fatal("invalid SQL execution input accepted")
			}
		})
	}
}

func TestExecutionRejectsIdentityAndRiskBeforeSecrets(t *testing.T) {
	changes := map[string]func(*connectors.RuntimeContext, *connectors.PreparedAction){
		"target-kind": func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Target.ConnectorKind = "sqlserver" },
		"profile-kind": func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) {
			r.Profile.ConnectorKind = "sqlserver"
		},
		"credential-kind":  func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Profile.Kind = "other" },
		"prepared-kind":    func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.ConnectorKind = "sqlserver" },
		"target-id":        func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Target.ID++ },
		"missing-target":   func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Target.ID = 0 },
		"profile-target":   func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Profile.TargetID++ },
		"profile-id":       func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Profile.ID++ },
		"missing-profile":  func(r *connectors.RuntimeContext, _ *connectors.PreparedAction) { r.Profile.ID = 0 },
		"prepared-profile": func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.ProfileID++ },
		"prepared-ref":     func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.TargetRef = "postgres:1:3" },
		"ref-padding": func(r *connectors.RuntimeContext, a *connectors.PreparedAction) {
			r.Target.Ref = " postgres:1:2"
			a.TargetRef = r.Target.Ref
		},
		"ref-alias": func(r *connectors.RuntimeContext, a *connectors.PreparedAction) {
			r.Target.Ref = "postgres:01:2"
			a.TargetRef = r.Target.Ref
		},
		"risk": func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.Risk = connectors.RiskDestructive },
		"read-write-risk": func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) {
			if a.ActionName == ActionExecuteSQL {
				a.Risk = connectors.RiskRead
			} else {
				a.Risk = connectors.RiskWrite
			}
		},
		"action": func(_ *connectors.RuntimeContext, a *connectors.PreparedAction) { a.ActionName = "unsupported" },
	}
	for _, name := range []string{ActionExecuteSQL, ActionQueryReadonly, ActionGetSchemas, ActionGetTables, ActionDescribeTable} {
		for change, mutate := range changes {
			t.Run(name+"/"+change, func(t *testing.T) {
				runtime := cleanupFixtureRuntime(nil)
				spy := &executeSecretSpy{}
				runtime.Secrets = spy
				input := map[string]any{"sql": "SELECT 1"}
				if name == ActionDescribeTable {
					input = map[string]any{"table": "items"}
				}
				action := prepareExecuteFixture(t, runtime, name, input)
				mutate(&runtime, &action)
				if _, err := New().ExecuteAction(t.Context(), runtime, action); err == nil || spy.calls != 0 {
					t.Fatalf("invalid execution reached secrets: calls=%d err=%v", spy.calls, err)
				}
			})
		}
	}
}

func TestExecutionRevalidatesPayloadBeforeSecrets(t *testing.T) {
	for _, name := range []string{ActionExecuteSQL, ActionQueryReadonly} {
		for _, sql := range []string{"", "SELECT 1\x00", strings.Repeat("x", sqlexecute.MaxSQLBytes+1)} {
			runtime := cleanupFixtureRuntime(nil)
			spy := &executeSecretSpy{}
			runtime.Secrets = spy
			action := prepareExecuteFixture(t, runtime, name, map[string]any{"sql": "SELECT 1"})
			action.Payload["sql"] = sql
			if _, err := New().ExecuteAction(t.Context(), runtime, action); err == nil || spy.calls != 0 {
				t.Fatal("invalid SQL payload reached credentials")
			}
		}
	}
	for _, field := range []string{"max_rows", "timeout_seconds"} {
		runtime := cleanupFixtureRuntime(nil)
		spy := &executeSecretSpy{}
		runtime.Secrets = spy
		action := prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": "SELECT 1"})
		action.Payload[field] = 0
		if _, err := New().ExecuteAction(t.Context(), runtime, action); err == nil || spy.calls != 0 {
			t.Fatal("invalid SQL limits reached credentials")
		}
	}
}

func TestExecutionReadonlyPolicyStillFailsClosed(t *testing.T) {
	for _, sql := range []string{
		"INSERT INTO public.items VALUES (1)", "CREATE TABLE public.items (id int)", "CALL public.p()", "DO $$BEGIN END$$",
		"SELECT 1; SELECT 2", "SELECT public.dangerous()", "SELECT pg_sleep(1)", "SELECT 1::public.custom",
	} {
		runtime := cleanupFixtureRuntime(nil)
		spy := &executeSecretSpy{}
		runtime.Secrets = spy
		action := prepareExecuteFixture(t, runtime, ActionQueryReadonly, map[string]any{"sql": "SELECT 1"})
		action.Payload["sql"] = sql
		if _, err := New().ExecuteAction(t.Context(), runtime, action); err == nil || spy.calls != 0 {
			t.Fatal("protected read policy accepted a write, routine, or unsafe function")
		}
	}
}

func TestExecuteSQLCanceledBeforeOpenDoesNotResolveSecrets(t *testing.T) {
	runtime := cleanupFixtureRuntime(nil)
	spy := &executeSecretSpy{}
	runtime.Secrets = spy
	action := prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": "UPDATE public.items SET value = 1"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := New().ExecuteAction(ctx, runtime, action)
	if err != context.Canceled || result.Status == connectors.ResultOutcomeUnknown || spy.calls != 0 {
		t.Fatal("pre-dispatch cancellation reached credentials or became an uncertain mutation")
	}
}

type executeSecretSpy struct{ calls int }

func (s *executeSecretSpy) GetSecret(context.Context, string) (string, error) {
	s.calls++
	return "fixture-only", nil
}
