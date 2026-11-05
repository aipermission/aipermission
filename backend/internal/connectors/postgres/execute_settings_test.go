package postgresconnector

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/jackc/pgx/v5/pgproto3"
)

func TestExecuteSQLDecodeFallbackDrainsRemainingCommands(t *testing.T) {
	for _, probe := range []struct {
		name, text string
		oid        uint32
	}{
		{"date", "31.01.2020", 1082}, {"bytea", "\\001", 17},
		{"interval", "1-2 +3 +4:05:06", 1186},
	} {
		t.Run(probe.name, func(t *testing.T) {
			f := newExecuteWireFixture(t, "SELECT changed_output; UPDATE items SET value=11;", func(b *pgproto3.Backend) (byte, error) {
				field := executeTextField("value")
				field.DataTypeOID = probe.oid
				sendExecuteRows(b, []pgproto3.FieldDescription{field}, [][]byte{[]byte(probe.text)})
				b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 1")})
				return 'I', nil
			})
			f.start()
			runtime := cleanupFixtureRuntime(f)
			result, err := New().ExecuteAction(t.Context(), runtime, prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": f.sql}))
			f.wait(t)
			if err != nil || result.Status != connectors.ResultCompleted || f.err != nil {
				t.Fatalf("decode interrupted script: %#v %v peer=%v", result, err, f.err)
			}
			out := result.Output.(map[string]any)
			encoded, _ := json.Marshal(out)
			quoted, _ := json.Marshal(probe.text)
			commands := out["commands"].([]sqlexecute.Command)
			if !strings.Contains(string(encoded), string(quoted)) || len(commands) != 2 || commands[1].Tag != "UPDATE 1" || len(out["notices"].([]string)) == 0 {
				t.Fatalf("server text or remaining command lost: %s", encoded)
			}
		})
	}
}

func TestExecuteSQLNonUTF8ResultsAreOmittedAndDrained(t *testing.T) {
	for _, encoding := range []string{"LATIN1", "UTF8"} {
		t.Run(encoding, func(t *testing.T) {
			f := newExecuteWireFixture(t, "SET client_encoding; SELECT value; UPDATE items SET value=11;", func(b *pgproto3.Backend) (byte, error) {
				b.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: encoding})
				sendExecuteRows(b, []pgproto3.FieldDescription{executeTextField("value")}, [][]byte{{0xe9}})
				b.Send(&pgproto3.NoticeResponse{Severity: "NOTICE", Message: string([]byte{0xe9})})
				b.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
				sendExecuteRows(b, []pgproto3.FieldDescription{executeTextField("restored")}, [][]byte{[]byte("valid UTF8")})
				b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 1")})
				return 'I', nil
			})
			f.start()
			runtime := cleanupFixtureRuntime(f)
			result, err := New().ExecuteAction(t.Context(), runtime, prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": f.sql}))
			f.wait(t)
			if err != nil || result.Status != connectors.ResultCompleted || f.err != nil {
				t.Fatalf("encoding interrupted script: %#v %v peer=%v", result, err, f.err)
			}
			out := result.Output.(map[string]any)
			encoded, _ := json.Marshal(out)
			if out["truncated"] != true || out["result_set_count"] != 2 || strings.Contains(string(encoded), "�") || !strings.Contains(string(encoded), "valid UTF8") || len(out["commands"].([]sqlexecute.Command)) != 3 {
				t.Fatalf("encoding corruption or lost drain: %s", encoded)
			}
		})
	}
}
