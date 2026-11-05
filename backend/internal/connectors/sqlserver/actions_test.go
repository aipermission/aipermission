package sqlserverconnector

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
)

func TestActionContractAndPreparation(t *testing.T) {
	runtime := fixtureRuntime()
	connectortest.AssertActionListStable(t, New(), runtime.Target, runtime.Profile)
	actions, err := New().GetActionList(t.Context(), runtime.Target, runtime.Profile)
	if err != nil || len(actions) != 6 {
		t.Fatalf("%#v %v", actions, err)
	}
	if err := connectors.ValidateActionDefinitions(actions, "SQL Server"); err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		input := map[string]any{}
		if action.Name == ActionDescribeTable {
			input["table"] = "order]; DROP TABLE x--"
		}
		if action.Name == ActionQueryReadonly || action.Name == ActionExecuteSQL {
			input["sql"] = "SELECT TOP (10) * FROM dbo.orders"
		}
		req := connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: action.Name, Input: input}
		connectortest.AssertPrepareActionDeterministic(t, New(), req)
		prepared, err := New().PrepareAction(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(prepared)
		if action.Name == ActionExecuteSQL {
			if prepared.Risk != connectors.RiskWrite || connectors.EffectiveRetryPolicy(action).Class != connectors.RetryNonIdempotent {
				t.Fatalf("unsafe write contract: %#v", prepared)
			}
			continue
		}
		if strings.Contains(string(encoded), "fixture-only") || prepared.Risk != connectors.RiskRead || connectors.EffectiveRetryPolicy(action).Class != connectors.RetryReadOnly {
			t.Fatalf("unsafe contract: %#v", prepared)
		}
		query, args, limit, err := actionQuery(prepared)
		if err != nil || query == "" || limit < 1 {
			t.Fatalf("invalid plan for %s", action.Name)
		}
		if action.Name == ActionDescribeTable && (strings.Contains(query, input["table"].(string)) || len(args) != 2) {
			t.Fatal("metadata identifier interpolated into SQL")
		}
	}
	for _, input := range []map[string]any{{"sql": "DELETE FROM dbo.orders"}, {"sql": "SELECT 1", "max_rows": 0}, {"sql": "SELECT 1", "max_rows": 1001}, {"sql": "SELECT 1", "max_rows": 1.5}, {"sql": "SELECT 1", "password": "no"}} {
		if _, err := New().PrepareAction(t.Context(), connectors.ActionRequest{ActionName: ActionQueryReadonly, Input: input}); err == nil {
			t.Fatalf("accepted %#v", input)
		}
	}
	if _, err := New().PrepareAction(t.Context(), connectors.ActionRequest{ActionName: "execute"}); err == nil {
		t.Fatal("unknown action accepted")
	}
	if _, err := New().PrepareAction(t.Context(), connectors.ActionRequest{ActionName: ActionDescribeTable, Input: map[string]any{"table": "x\x00"}}); err == nil {
		t.Fatal("null identifier accepted")
	}
	if help, err := New().GetHelp(t.Context(), runtime.Target); err != nil || help.ConnectorID != Kind || len(help.Warnings) == 0 {
		t.Fatal("missing safety guidance")
	}
}

func TestExecuteRevalidatesBeforeSecretDelivery(t *testing.T) {
	runtime := fixtureRuntime()
	prepared, err := New().PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionQueryReadonly, Input: map[string]any{"sql": "SELECT 1"}})
	if err != nil {
		t.Fatal(err)
	}
	prepared.Payload["sql"] = "DELETE FROM dbo.orders"
	if _, err := New().ExecuteAction(t.Context(), runtime, prepared); err == nil {
		t.Fatal("execution did not revalidate SQL")
	}
	prepared.ProfileID = 999
	if _, err := New().ExecuteAction(t.Context(), runtime, prepared); err == nil {
		t.Fatal("execution accepted another profile")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	db, err := connection(ctx, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err == nil {
		t.Fatal("cancellation lost")
	}
}

func FuzzReadonlySQL(f *testing.F) {
	for _, query := range []string{"SELECT 1", "SELECT TOP (2) * FROM [dbo].[orders]", "WITH c AS (SELECT 1 AS x) SELECT * FROM c", "SELECT * INTO dbo.x FROM dbo.y", "SELECT x FROM dbo.x ORDER BY dbo.f()"} {
		f.Add(query)
	}
	data, err := os.ReadFile("testdata/readonly-attacks.json")
	if err != nil {
		f.Fatal(err)
	}
	var attacks []string
	if err := json.Unmarshal(data, &attacks); err != nil {
		f.Fatal(err)
	}
	for _, query := range attacks {
		f.Add(query)
	}
	f.Add(`SELECT "a""b" FROM dbo.t`)
	f.Add("SELECT 1 " + strings.Repeat("/*", 512) + strings.Repeat("*/", 512))
	f.Fuzz(func(t *testing.T, query string) {
		compiled, err := compileReadonlySQL(query)
		if err != nil {
			return
		}
		again, err := compileReadonlySQL(compiled)
		if err != nil || again != compiled {
			t.Fatalf("projection is not idempotent: %q -> %q -> %q: %v", query, compiled, again, err)
		}
	})
}
