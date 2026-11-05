package postgresconnector

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
	"github.com/jackc/pgx/v5/pgproto3"
)

func TestExecuteSQLNativeScriptAndNormalizedResults(t *testing.T) {
	script := "\nBEGIN; CREATE TABLE public.items (value bytea); INSERT INTO public.items VALUES ('\\xff'); COMMIT;\n" +
		"CREATE PROCEDURE public.p() LANGUAGE plpgsql AS $$BEGIN RAISE NOTICE 'a;b'; END$$; CALL public.p(); DO $b$BEGIN PERFORM 1; END$b$;\nSELECT * FROM public.items;\n"
	f := newExecuteWireFixture(t, script, func(b *pgproto3.Backend) (byte, error) {
		for _, tag := range []string{"BEGIN", "CREATE TABLE", "INSERT 0 1", "COMMIT", "CREATE PROCEDURE", "CALL", "DO"} {
			b.Send(&pgproto3.CommandComplete{CommandTag: []byte(tag)})
		}
		b.Send(&pgproto3.NoticeResponse{Severity: "NOTICE", Message: "a;b"})
		fields := []pgproto3.FieldDescription{executeTextField("binary"), executeTextField("stamp"), executeTextField("integer"), executeTextField("null")}
		fields[0].DataTypeOID, fields[1].DataTypeOID, fields[2].DataTypeOID = 17, 1184, 20
		sendExecuteRows(b, fields, [][]byte{[]byte(`\xff0041`), []byte("2026-11-05 12:30:00+03"), []byte("9223372036854775807"), nil})
		sendExecuteRows(b, []pgproto3.FieldDescription{executeTextField("value")}, [][]byte{[]byte("next result")})
		return 'I', nil
	})
	f.start()
	runtime := cleanupFixtureRuntime(f)
	action := prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": script, "timeout_seconds": 300})
	result, err := New().ExecuteAction(t.Context(), runtime, action)
	f.wait(t)
	if err != nil || result.Status != connectors.ResultCompleted || f.err != nil || f.scripts != 1 || f.dials.Load() != 1 {
		t.Fatalf("native SQL execution failed: status=%s err=%v peer=%v scripts=%d dials=%d", result.Status, err, f.err, f.scripts, f.dials.Load())
	}
	if !reflect.DeepEqual(f.timeouts, []string{"300000ms"}) || len(f.simple) != 2 || f.simple[1] != script {
		t.Fatal("write timeout or unchanged single-query dispatch was lost, or a transaction wrapper was added")
	}
	output := result.Output.(map[string]any)
	sets := output["result_sets"].([]sqlresult.Result)
	if len(sets) != 2 || output["completed_batches"] != 1 || output["transaction_control"] != "script_defined" || output["retry_safe"] != false {
		t.Fatalf("unexpected canonical SQL stream shape: %#v", output)
	}
	want := map[string]any{"binary": `\xff0041`, "stamp": "2026-11-05T09:30:00Z", "integer": int64(9223372036854775807), "null": nil}
	if !reflect.DeepEqual(sets[0].Rows[0], want) || !reflect.DeepEqual(output["notices"], []string{"a;b"}) {
		t.Fatalf("Postgres values or notices changed: %#v", output)
	}
	commands := output["commands"].([]sqlexecute.Command)
	if len(commands) != 9 || commands[2].Tag != "INSERT 0 1" || commands[2].ReportedRows != "1" || commands[5].Tag != "CALL" {
		t.Fatalf("command acknowledgements lost: %#v", commands)
	}
}

func TestExecuteSQLTruncationDrainsLaterCommands(t *testing.T) {
	for name, reply := range map[string]func(*pgproto3.Backend) (byte, error){
		"rows": executeRowCapReply, "columns": executeColumnCapReply, "results": executeResultCapReply, "bytes": executeByteCapReply,
	} {
		t.Run(name, func(t *testing.T) {
			script := "SELECT * FROM public.items; UPDATE public.items SET value = 9;"
			f := newExecuteWireFixture(t, script, reply)
			f.start()
			runtime := cleanupFixtureRuntime(f)
			limit := 1000
			if name == "rows" {
				limit = 1
			}
			action := prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": script, "max_rows": limit})
			result, err := New().ExecuteAction(t.Context(), runtime, action)
			f.wait(t)
			if err != nil || result.Status != connectors.ResultCompleted || f.err != nil {
				t.Fatalf("truncation interrupted SQL: status=%s err=%v peer=%v", result.Status, err, f.err)
			}
			output := result.Output.(map[string]any)
			commands := output["commands"].([]sqlexecute.Command)
			if output["truncated"] != true || len(commands) == 0 || commands[len(commands)-1].Tag != "UPDATE 9" || commands[len(commands)-1].ReportedRows != "9" {
				t.Fatalf("truncation skipped later script acknowledgements: %#v", output)
			}
			encoded, err := json.Marshal(output)
			if err != nil || len(encoded) > sqlexecute.MaxOutputBytes {
				t.Fatal("SQL output exceeded its serialization budget")
			}
		})
	}
}

func executeRowCapReply(b *pgproto3.Backend) (byte, error) {
	field := executeTextField("value")
	field.DataTypeOID = 23
	// The third row and next result cannot decode. They must be drained, not decoded.
	sendExecuteRows(b, []pgproto3.FieldDescription{field}, [][]byte{[]byte("1")}, [][]byte{[]byte("2")}, [][]byte{[]byte("not an integer")})
	sendExecuteRows(b, []pgproto3.FieldDescription{field}, [][]byte{[]byte("also not an integer")})
	b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 9")})
	return 'I', nil
}

func executeColumnCapReply(b *pgproto3.Backend) (byte, error) {
	fields, row := make([]pgproto3.FieldDescription, 257), make([][]byte, 257)
	for i := range fields {
		fields[i] = executeTextField(fmt.Sprintf("column_%d", i))
		fields[i].DataTypeOID, row[i] = 23, []byte("not an integer")
	}
	sendExecuteRows(b, fields, row)
	b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 9")})
	return 'I', nil
}

func executeResultCapReply(b *pgproto3.Backend) (byte, error) {
	for i := 0; i < 32; i++ {
		sendExecuteRows(b, []pgproto3.FieldDescription{executeTextField("value")})
	}
	field := executeTextField("value")
	field.DataTypeOID = 23
	sendExecuteRows(b, []pgproto3.FieldDescription{field}, [][]byte{[]byte("not an integer")})
	b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 9")})
	return 'I', nil
}

func executeByteCapReply(b *pgproto3.Backend) (byte, error) {
	rows := make([][][]byte, 20)
	for i := range rows {
		rows[i] = [][]byte{[]byte(strings.Repeat("x", 60000))}
	}
	sendExecuteRows(b, []pgproto3.FieldDescription{executeTextField("value")}, rows...)
	for i := 0; i < 40; i++ {
		b.Send(&pgproto3.NoticeResponse{Severity: "NOTICE", Message: strings.Repeat("n", 1000)})
	}
	b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 9")})
	return 'I', nil
}

func TestExecuteSQLCellsRemainBoundedAndNormalized(t *testing.T) {
	script := "SELECT * FROM public.large_cells;"
	f := newExecuteWireFixture(t, script, func(b *pgproto3.Backend) (byte, error) {
		binaryField := executeTextField("binary")
		binaryField.DataTypeOID = 17
		sendExecuteRows(b, []pgproto3.FieldDescription{executeTextField("text"), binaryField},
			[][]byte{[]byte(strings.Repeat("x", 100000)), []byte(`\x` + strings.Repeat("ff", 100000))})
		return 'I', nil
	})
	f.start()
	runtime := cleanupFixtureRuntime(f)
	result, err := New().ExecuteAction(t.Context(), runtime, prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": script}))
	f.wait(t)
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("large cells failed: status=%s err=%v", result.Status, err)
	}
	output := result.Output.(map[string]any)
	row := output["result_sets"].([]sqlresult.Result)[0].Rows[0]
	for _, name := range []string{"text", "binary"} {
		value := row[name].(string)
		if len(value) > sqlexecute.MaxCellBytes || !strings.HasSuffix(value, truncatedSuffix) || output["truncated"] != true {
			t.Fatal("large cells escaped explicit bounds")
		}
	}
}

func TestExecuteSQLPostDispatchFailuresAreUnknown(t *testing.T) {
	for _, mode := range []string{"sql-error", "lost-reply", "malformed-row", "wire-cap", "psql-directive"} {
		t.Run(mode, func(t *testing.T) {
			script := "UPDATE public.items SET value = 1; COMMIT; SELECT * FROM public.items;"
			if mode == "psql-directive" {
				script = "\\! never-execute-a-shell\n"
			}
			f := newExecuteWireFixture(t, script, nil)
			f.reply = executeFailureReply(mode, f.server)
			f.start()
			runtime := cleanupFixtureRuntime(f)
			action := prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": script})
			result, err := New().ExecuteAction(t.Context(), runtime, action)
			f.wait(t)
			assertExecuteUnknown(t, result, err)
			// pgx may attempt a separate cancellation transport, not replay SQL.
			if f.scripts != 1 || f.dials.Load() > 2 {
				t.Fatal("failed SQL was retried")
			}
		})
	}
}

func executeFailureReply(mode string, wire io.Writer) func(*pgproto3.Backend) (byte, error) {
	return func(b *pgproto3.Backend) (byte, error) {
		b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 1")})
		switch mode {
		case "lost-reply":
			return 'I', io.ErrUnexpectedEOF
		case "malformed-row":
			// Presentation decode failures retain server text; a structurally
			// invalid protocol row remains fatal and must never become success.
			sendExecuteRows(b, []pgproto3.FieldDescription{executeTextField("value")}, [][]byte{[]byte("1"), []byte("unexpected extra field")})
		case "wire-cap":
			if err := b.Flush(); err != nil {
				return 'I', err
			}
			var header [5]byte
			header[0] = 'D'
			binary.BigEndian.PutUint32(header[1:], maxProtocolMessageBytes+5)
			_, err := wire.Write(header[:])
			return 'I', err
		default:
			b.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "42601", Message: "fixture SQL syntax error"})
		}
		return 'I', nil
	}
}

func assertExecuteUnknown(t *testing.T, result connectors.ActionResult, err error) {
	t.Helper()
	if err != nil || result.Status != connectors.ResultOutcomeUnknown {
		t.Fatalf("post-dispatch failure not conservative: status=%s err=%v", result.Status, err)
	}
	output := result.Output.(map[string]any)
	if output["retry_safe"] != false || output["dispatch_stage"] != "sql_execution" || result.Error == "" {
		t.Fatalf("uncertain SQL lost its retry/dispatch contract: %#v", output)
	}
}

func TestExecuteSQLUnfinishedTransactionsRollbackAndClose(t *testing.T) {
	for _, status := range []byte{'T', 'E'} {
		for _, silent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%c/silent=%v", status, silent), func(t *testing.T) {
				script := "BEGIN; UPDATE public.items SET value = 1;"
				f := newExecuteWireFixture(t, script, func(b *pgproto3.Backend) (byte, error) {
					b.Send(&pgproto3.CommandComplete{CommandTag: []byte("BEGIN")})
					b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 1")})
					return status, nil
				})
				f.silentRollback = silent
				f.client = noDeadlineExecutionConn{f.client}
				f.start()
				runtime := cleanupFixtureRuntime(f)
				start := time.Now()
				result, err := New().ExecuteAction(t.Context(), runtime, prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": script}))
				f.wait(t)
				assertExecuteUnknown(t, result, err)
				select {
				case <-f.rollback:
				default:
					t.Fatal("non-idle SQL session was not rolled back")
				}
				if time.Since(start) > 5*time.Second || f.scripts != 1 || f.dials.Load() > 2 {
					t.Fatal("transaction cleanup was unbounded or retried")
				}
			})
		}
	}
}

func TestExecuteSQLAuthenticationFailureIsPreDispatch(t *testing.T) {
	f := newExecuteWireFixture(t, "UPDATE public.items SET value = 1;", nil)
	f.rejectAuth = true
	f.start()
	runtime := cleanupFixtureRuntime(f)
	result, err := New().ExecuteAction(t.Context(), runtime, prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": f.sql}))
	f.wait(t)
	if err == nil || result.Status == connectors.ResultOutcomeUnknown || f.scripts != 0 || f.dials.Load() != 1 {
		t.Fatal("authentication did not complete before script dispatch, or was retried")
	}
}

func TestReadonlyExecutionKeepsTransactionAndTimeout(t *testing.T) {
	f := newExecuteWireFixture(t, "never dispatched as a simple script", nil)
	f.start()
	runtime := cleanupFixtureRuntime(f)
	action := prepareExecuteFixture(t, runtime, ActionQueryReadonly, map[string]any{"sql": "SELECT 'fixture' AS value"})
	result, err := New().ExecuteAction(t.Context(), runtime, action)
	f.wait(t)
	if err != nil || result.Status != connectors.ResultCompleted || f.err != nil {
		t.Fatalf("valid protected read failed: status=%s err=%v peer=%v", result.Status, err, f.err)
	}
	if !reflect.DeepEqual(f.timeouts, []string{"20000ms"}) || !containsString(f.simple, "begin read only") || !containsString(f.simple, "commit") {
		t.Fatal("protected read transaction or timeout default changed")
	}
	if !containsString(f.simple, "SELECT set_config('search_path', 'pg_catalog', true)") || len(f.extended) < 3 || !strings.Contains(strings.Join(f.extended, "\n"), "pg_catalog.pg_cast") {
		t.Fatal("protected read function-resolution policy was bypassed")
	}
}

func TestExecuteSQLCancellationAndDeadline(t *testing.T) {
	for _, mode := range []string{"cancel", "selected-timeout", "caller-deadline"} {
		t.Run(mode, func(t *testing.T) {
			script := "UPDATE public.items SET value = 1; SELECT 'blocked';"
			f := newExecuteWireFixture(t, script, func(b *pgproto3.Backend) (byte, error) {
				b.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 1")})
				if err := b.Flush(); err != nil {
					return 'I', err
				}
				_, err := b.Receive()
				return 'I', err
			})
			f.client = noDeadlineExecutionConn{f.client}
			f.start()
			runtime := cleanupFixtureRuntime(f)
			action := prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": script, "timeout_seconds": 1})
			ctx, cancel := context.WithCancel(t.Context())
			if mode == "caller-deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 200*time.Millisecond)
			}
			defer cancel()
			done := make(chan connectors.ActionResult, 1)
			go func() { result, _ := New().ExecuteAction(ctx, runtime, action); done <- result }()
			select {
			case <-f.started:
			case <-time.After(2 * time.Second):
				t.Fatal("SQL was not dispatched")
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case result := <-done:
				assertExecuteUnknown(t, result, nil)
			case <-time.After(5 * time.Second):
				t.Fatal("SQL cancellation/deadline retained the action")
			}
			f.wait(t)
			if f.scripts != 1 || f.dials.Load() > 2 || !reflect.DeepEqual(f.timeouts, []string{"1000ms"}) {
				t.Fatal("SQL timeout configuration changed or cancellation retried")
			}
		})
	}
}

// SSH channels deliberately do not support socket deadlines.
type noDeadlineExecutionConn struct{ net.Conn }

func (noDeadlineExecutionConn) SetDeadline(time.Time) error {
	return fmt.Errorf("deadline unsupported")
}

func (noDeadlineExecutionConn) SetReadDeadline(time.Time) error {
	return fmt.Errorf("deadline unsupported")
}

func (noDeadlineExecutionConn) SetWriteDeadline(time.Time) error {
	return fmt.Errorf("deadline unsupported")
}
