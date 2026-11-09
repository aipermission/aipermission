package sqlserverconnector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
)

func TestExecutionSQLAcceptsBroadAuthorityWithoutRewriting(t *testing.T) {
	for _, script := range []string{
		"  -- approval bytes\r\nINSERT INTO dbo.t (v) VALUES (N'\u00e7\u0131\u011f GO'); UPDATE dbo.t SET v=N'\u65e5\u672c'; DELETE FROM dbo.t;  ",
		"BEGIN TRANSACTION; CREATE TABLE #t (id int); ALTER TABLE #t ADD v nvarchar(32); INSERT INTO #t VALUES(1, N'x'); COMMIT;",
		"EXEC dbo.do_work @value = N'x'; EXEC(N'SELECT 1');",
		"CREATE OR ALTER PROCEDURE dbo.p AS BEGIN PRINT N'hello'; SELECT 1 AS x; END;",
		"CREATE VIEW dbo.v AS SELECT 1 AS x;",
		"GRANT SELECT ON dbo.t TO public;",
		"SELECT \"a\"\"b\", [GO], N'GO\n:r literal' FROM dbo.t;",
		"/* \u65e5\u672c /* nested */ comment */ SELECT 1; -- \u00e7\u0131\u011f\n",
		"RAISERROR(N'owned-batch-error',16,1) WITH NOWAIT; WAITFOR DELAY '00:00:01'; UPDATE dbo.[t] SET value=7 WHERE id=1;\nGO\nUPDATE dbo.[t] SET value=100 WHERE id=1;",
		"INSERT INTO dbo.[t] VALUES (1,99); UPDATE dbo.[t] SET value=7 WHERE id=1;\nGO\nUPDATE dbo.[t] SET value=100 WHERE id=1;",
		"SELECT SERVERPROPERTY('ProductMajorVersion'); UPDATE dbo.[t] SET value=7 WHERE id=1;\nGO\nUPDATE dbo.[t] SET value=100 WHERE id=1;",
	} {
		t.Run(script, func(t *testing.T) {
			batches, err := parseExecutionSQL(t.Context(), script)
			if err != nil || strings.Join(batches, "GO") != script {
				t.Fatalf("SQL changed: %#v, %v", batches, err)
			}
			batches, err = compileExecutionSQL(t.Context(), script)
			if errors.Is(err, context.DeadlineExceeded) {
				if len(batches) != 0 || !strings.Contains(err.Error(), "no SQL was sent") {
					t.Fatalf("admission timeout lost fail-closed evidence: %#v %v", batches, err)
				}
			} else if err != nil || strings.Join(batches, "GO") != script {
				t.Fatalf("bounded admission changed SQL: %#v %v", batches, err)
			}
		})
	}
}

func TestExecutionGOBatchesPreserveApprovalByteSlices(t *testing.T) {
	first := "-- \u65e5\u672c\r\nCREATE TABLE dbo.t (id int);\r\n /*before*/ "
	second := " -- separator comment\r\nCREATE PROCEDURE dbo.p AS SELECT N'GO';\r\n"
	third := "\nEXEC dbo.p; /*\u0131*/\n"
	script := first + "gO" + second + "GO" + third
	batches, err := compileExecutionSQL(t.Context(), script)
	if err != nil || len(batches) != 3 {
		t.Fatalf("batches=%#v, err=%v", batches, err)
	}
	for i, want := range []string{first, second, third} {
		if batches[i] != want {
			t.Errorf("batch %d changed: %q != %q", i, batches[i], want)
		}
	}
	trailing := "SELECT 1;\nGO -- \u65e5\u672c\n"
	batches, err = compileExecutionSQL(t.Context(), trailing)
	if err != nil || len(batches) != 2 || batches[0] != "SELECT 1;\n" || batches[1] != " -- \u65e5\u672c\n" {
		t.Fatalf("trailing comments lost: %#v %v", batches, err)
	}
}

func TestExecutionSQLRejectsClientDirectivesAndBoundedMalformedInput(t *testing.T) {
	for _, script := range []string{
		"", "-- only comment", "\xff", "SELECT 1\x00", "SELECT 1;\nGO 2\nSELECT 2;", "SELECT 1;\nGO 1", "SELECT 1; GO SELECT 2;",
		"GO\nSELECT 1;", "SELECT 1;\nGO\nGO\nSELECT 2;", ":r other.sql", "/*x*/ :setvar x y\nSELECT 1;", "!! echo unsafe", "SELECT $(value);",
		"SELECT 1;\n:connect host", "SELECT 1;\n:on error ignore", "SELECT 'open", "SELECT [open", "SELECT 1; /* open", "SELECT 1; garbage ???",
		"SELECT " + strings.Repeat("(", 33) + "1" + strings.Repeat(")", 33),
		"SELECT 1 " + strings.Repeat("/*", 17) + strings.Repeat("*/", 17),
		strings.Repeat("SELECT 1;\nGO\n", 33), strings.Repeat(";", 8193), strings.Repeat("x", sqlexecute.MaxSQLBytes+1),
	} {
		if batches, err := compileExecutionSQL(t.Context(), script); err == nil {
			t.Fatalf("accepted %q as %#v", script[:min(len(script), 80)], batches)
		}
	}
}

func TestExecutionSQLLimitsAndCanonicalPreparation(t *testing.T) {
	runtime := fixtureRuntime()
	script := "  /* \u65e5\u672c */ SELECT 1;\nGO\nEXEC dbo.p;  "
	req := connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionExecuteSQL, Input: map[string]any{"sql": script, "timeout_seconds": 300, "max_rows": 1}}
	connectortest.AssertPrepareActionDeterministic(t, New(), req)
	prepared, err := New().PrepareAction(t.Context(), req)
	if err != nil || text(prepared.Payload, "sql") != script || text(prepared.Preview, "sql") != script || prepared.Risk != connectors.RiskWrite {
		t.Fatalf("canonical approval changed: %#v %v", prepared, err)
	}
	for _, input := range []map[string]any{
		{"sql": script, "timeout_seconds": 301}, {"sql": script, "timeout_seconds": 0}, {"sql": script, "timeout_seconds": 1.5},
		{"sql": script, "max_rows": 1001}, {"sql": script, "max_rows": 0}, {"sql": script, "password": "no"},
	} {
		req.Input = input
		if _, err := New().PrepareAction(t.Context(), req); err == nil {
			t.Fatalf("accepted invalid input %#v", input)
		}
	}
	for _, script := range []string{strings.Repeat("SELECT 1;\nGO\n", 32), "SELECT 1;" + strings.Repeat(" ", sqlexecute.MaxSQLBytes-len("SELECT 1;"))} {
		if _, err := compileExecutionSQL(t.Context(), script); err != nil {
			t.Fatalf("rejected boundary: %v", err)
		}
	}
}

func TestExecutionParserWorkAndCancellationAreBounded(t *testing.T) {
	chain := "IF @a = 0 PRINT 0;\n" + strings.Repeat("ELSE IF @a = 0 PRINT 0;\n", 75) + "ELSE PRINT 1;\n"
	script := "DECLARE @a int = 1;\n" + strings.Repeat(chain, 10)
	started := time.Now()
	batches, err := parseExecutionSQL(t.Context(), script)
	if err != nil || len(batches) != 1 || batches[0] != script {
		t.Fatalf("bounded valid parser input: batches=%d err=%v elapsed=%v", len(batches), err, time.Since(started))
	}
	t.Logf("18350-byte ambiguous script accepted within syntax work bounds in %s (not a wall-clock benchmark)", time.Since(started))
	canceled, stop := context.WithCancel(t.Context())
	stop()
	if _, err := compileExecutionSQL(canceled, script); !errors.Is(err, context.Canceled) {
		t.Fatalf("parser ignored cancellation: %v", err)
	}
}

func TestExecutionTokenStreamStopsAtWorkOrContextBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, stream := range []*executionTokenStream{
		{ctx: t.Context(), operations: executionParserOperations}, {ctx: ctx},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("parser work continued after its boundary")
				}
			}()
			stream.check()
		}()
	}
}
