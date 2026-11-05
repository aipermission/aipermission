package sqlserverconnector

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/golang-sql/sqlexp"
)

func runExecutionFixture(ctx context.Context, f *executionFixture, script string, rows, seconds int) (connectors.ActionResult, error) {
	runtime := fixtureRuntime()
	prepared, err := New().PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionExecuteSQL, Input: map[string]any{"sql": script, "max_rows": rows, "timeout_seconds": seconds}})
	if err != nil {
		return connectors.ActionResult{}, err
	}
	batches, err := compileExecutionSQL(ctx, script)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return sqlexecute.Run(ctx, prepared, batches, func(ctx context.Context) (sqlexecute.Session, error) {
		return openExecutionSession(ctx, f, seconds)
	})
}

func scalarExecutionResult(values ...driver.Value) executionResultFixture {
	result := executionResultFixture{columns: []string{"value"}, kinds: []string{"INT"}}
	for _, value := range values {
		result.values = append(result.values, []driver.Value{value})
	}
	return result
}

func TestNativeExecutionDrainsTruncatedResultsAndPreservesMessages(t *testing.T) {
	first := scalarExecutionResult(int64(1), int64(2), int64(3))
	first.notices, first.count = 40, fixtureCount(3)
	last := scalarExecutionResult(int64(99))
	last.count = fixtureCount(1)
	f := &executionFixture{plans: [][]executionResultFixture{{first, {count: fixtureCount(7)}, last}, {{count: fixtureCount(12)}}}}
	script := " /* \u65e5\u672c */ SELECT 1; UPDATE dbo.t SET id=2; SELECT 99;\nGO\nEXEC dbo.p;  "
	result, err := runExecutionFixture(t.Context(), f, script, 1, 120)
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	batches, _ := compileExecutionSQL(t.Context(), script)
	if !reflect.DeepEqual(f.queries, batches) || f.reads != 4 || f.connects != 1 || f.closes != 1 || f.finishes != 1 {
		t.Fatalf("script/session lifecycle changed: %#v", f)
	}
	if len(f.setups) != 1 || strings.Contains(f.setups[0], "ROWCOUNT") || !strings.Contains(f.setups[0], "LOCK_TIMEOUT 120000") || time.Until(f.deadline) < 110*time.Second {
		t.Fatalf("write capped or row limited: %#v", f)
	}
	output := result.Output.(map[string]any)
	commands := output["commands"].([]sqlexecute.Command)
	if output["row_count"] != 1 || output["result_set_count"] != 2 || output["completed_batches"] != 2 || output["truncated"] != true || output["transaction_control"] != "script_defined" || len(commands) != 4 {
		t.Fatalf("output=%#v", output)
	}
	if commands[0].Tag != "server_done_count" || commands[0].ReportedRows != "3" || commands[1].ReportedRows != "7" || commands[3].ReportedRows != "12" || len(output["notices"].([]string)) != 32 {
		t.Fatalf("messages lost or affected rows fabricated: %#v", output)
	}
	encoded, _ := json.Marshal(output)
	if len(encoded) > sqlexecute.MaxOutputBytes {
		t.Fatal("output exceeded serialized bounds")
	}
}

func TestNativeExecutionColumnAndResultCapsDoNotSkipLaterCommands(t *testing.T) {
	wide := executionResultFixture{columns: make([]string, 257), kinds: make([]string, 257), values: [][]driver.Value{make([]driver.Value, 257)}}
	for i := range wide.kinds {
		wide.kinds[i] = "INT"
	}
	for _, first := range []executionResultFixture{wide, scalarExecutionResult(int64(1))} {
		plan := []executionResultFixture{first}
		for range 40 {
			plan = append(plan, scalarExecutionResult(int64(2)))
		}
		plan = append(plan, executionResultFixture{count: fixtureCount(7)})
		f := &executionFixture{plans: [][]executionResultFixture{plan}}
		result, err := runExecutionFixture(t.Context(), f, "EXEC dbo.p;", 100, 30)
		if err != nil || result.Status != connectors.ResultCompleted || f.reads != 41 || f.finishes != 1 {
			t.Fatalf("output cap interrupted script: %#v %v reads=%d", result, err, f.reads)
		}
		output := result.Output.(map[string]any)
		commands := output["commands"].([]sqlexecute.Command)
		if output["truncated"] != true || output["result_set_count"] != 41 || len(commands) != 1 || commands[0].ReportedRows != "7" {
			t.Fatalf("later command lost: %#v", output)
		}
	}
	collector := sqlexecute.NewCollector(1)
	_ = collector.BeginResult(make([]string, 257))
	if err := retainExecutionRow(&executionSink{Collector: collector}, []driver.Value{int64(1)}, nil); err != nil {
		t.Fatal("discarded rows must not normalize or inspect values")
	}
}

func TestNativeExecutionUnsupportedTypesAbortBeforeConnectorNext(t *testing.T) {
	for _, kind := range []string{"XML", "UDT", "SQL_VARIANT", "NEW_UNREVIEWED_TYPE"} {
		unsafe := scalarExecutionResult("unsafe")
		unsafe.kinds[0] = kind
		f := &executionFixture{plans: [][]executionResultFixture{{scalarExecutionResult(int64(1)), unsafe}, {{count: fixtureCount(7)}}}}
		result, err := runExecutionFixture(t.Context(), f, "SELECT 1;\nGO\nUPDATE dbo.t SET id=2;", 1, 30)
		if err != nil || result.Status != connectors.ResultOutcomeUnknown || f.reads != 1 || f.finishes != 0 || f.closes != 1 || len(f.queries) != 1 {
			t.Fatalf("%s was materialized or reported completed: %#v %v %#v", kind, result, err, f)
		}
	}
}

func TestNativeExecutionFailuresAreUnknownAndNeverRetried(t *testing.T) {
	failure := errors.New("owned fixture failure")
	for name, f := range map[string]*executionFixture{
		"query dispatch":  {queryErr: driver.ErrBadConn},
		"stream":          {plans: [][]executionResultFixture{{{columns: []string{"x"}, kinds: []string{"INT"}, rowErr: failure}}}},
		"server":          {plans: [][]executionResultFixture{{{message: sqlexp.MsgError{Error: failure}}}}},
		"empty error":     {plans: [][]executionResultFixture{{{message: sqlexp.MsgError{}}}}},
		"unknown message": {plans: [][]executionResultFixture{{{message: sqlexp.MsgLastInsertID{Value: int64(1)}}}}},
		"finish":          {plans: [][]executionResultFixture{{{count: fixtureCount(7)}}}, finishErr: failure},
		"messages":        {messageCheckErr: failure},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := runExecutionFixture(t.Context(), f, "UPDATE dbo.t SET id=1;", 1, 30)
			if err != nil || result.Status != connectors.ResultOutcomeUnknown || f.connects != 1 || f.closes != 1 || len(f.queries) > 1 {
				t.Fatalf("mutation retried or failure hidden: %#v %v %#v", result, err, f)
			}
		})
	}
}

func TestNativeExecutionOpenTransactionsAreNotClaimedAtomic(t *testing.T) {
	for _, state := range [][2]int64{{1, 1}, {1, -1}, {0, -1}} {
		f := &executionFixture{plans: [][]executionResultFixture{{{count: fixtureCount(7)}}}, transactionCount: state[0], transactionState: state[1]}
		result, err := runExecutionFixture(t.Context(), f, "BEGIN TRAN; UPDATE dbo.t SET id=2;", 1, 30)
		if err != nil || result.Status != connectors.ResultOutcomeUnknown || f.finishes != 1 || f.closes != 1 || len(f.queries) != 1 {
			t.Fatalf("open transaction accepted: %#v %v %#v", result, err, f)
		}
	}
}

func TestNativeExecutionPredispatchFailuresRemainOrdinary(t *testing.T) {
	failure := errors.New("owned authentication/init failure")
	for _, f := range []*executionFixture{{connectErr: failure}, {setupErr: failure}, {prepareErr: failure}, {stmtCloseErr: failure}} {
		result, err := runExecutionFixture(t.Context(), f, "EXEC dbo.p;", 1, 30)
		if err == nil || result.Status == connectors.ResultOutcomeUnknown || len(f.queries) != 0 || f.connects != 1 || f.closes != 1 {
			t.Fatalf("predispatch failure classification: %#v %v %#v", result, err, f)
		}
	}
}

func TestNativeExecutionCallerDeadlineWinsOverSelectedTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	f := &executionFixture{block: true}
	result, err := runExecutionFixture(ctx, f, "EXEC dbo.p;", 1, 300)
	if err != nil || result.Status != connectors.ResultOutcomeUnknown || f.connects != 1 || f.closes != 1 || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("deadline did not stop the attempt: %#v %v", result, err)
	}
}
