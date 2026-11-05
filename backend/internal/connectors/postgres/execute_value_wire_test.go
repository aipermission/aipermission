package postgresconnector

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5/pgproto3"
)

func TestExecuteSQLWireKeepsInfinityAndJSONNumberIdentity(t *testing.T) {
	script := "SELECT date_value, json_value FROM public.values;"
	f := newExecuteWireFixture(t, script, func(backend *pgproto3.Backend) (byte, error) {
		date, value := executeTextField("date_value"), executeTextField("json_value")
		date.DataTypeOID, value.DataTypeOID = 1082, 3802
		sendExecuteRows(backend, []pgproto3.FieldDescription{date, value}, [][]byte{[]byte("infinity"), []byte(`{"n":9007199254740993}`)})
		return 'I', nil
	})
	f.start()
	runtime := cleanupFixtureRuntime(f)
	result, err := New().ExecuteAction(t.Context(), runtime, prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": script}))
	f.wait(t)
	if err != nil || result.Status != connectors.ResultCompleted || f.err != nil {
		t.Fatalf("value execution failed: %#v %v peer=%v", result, err, f.err)
	}
	public, err := actionresult.Canonicalize(result.Output, actionresult.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(public)
	if err != nil || !strings.Contains(string(encoded), `"date_value":"infinity"`) || !strings.Contains(string(encoded), `"n":"9007199254740993"`) {
		t.Fatalf("native values lost identity before publication: %s %v", encoded, err)
	}
}
