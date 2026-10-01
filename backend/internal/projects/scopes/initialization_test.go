package scopes

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type initializationExecutor struct {
	sqldb.Executor
	calls int
	ctx   context.Context
	query string
	args  []any
	err   error
}

func (executor *initializationExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	executor.calls++
	executor.ctx, executor.query, executor.args = ctx, query, args
	return nil, executor.err
}

func TestInitializationPreservesExecutorContextAndOriginalTimestamps(t *testing.T) {
	executor := &initializationExecutor{}
	if err := InitializeForToken(t.Context(), executor, 7, "created", "updated"); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || executor.ctx != t.Context() || !reflect.DeepEqual(executor.args, []any{int64(7), "created", "updated"}) {
		t.Fatalf("initialization changed caller binding: %#v", executor)
	}
	if !strings.Contains(executor.query, "WHERE status = 'active'") {
		t.Fatal("scope initialization includes archived projects")
	}
}

func TestInitializationRejectsInvalidIDBeforeWriteAndPreservesFailure(t *testing.T) {
	for _, id := range []int64{0, -1} {
		executor := &initializationExecutor{}
		if err := InitializeForToken(t.Context(), executor, id, "created", "updated"); err == nil || executor.calls != 0 {
			t.Fatalf("invalid token wrote scopes: %v", err)
		}
	}
	failure := errors.New("fixture scope failure")
	executor := &initializationExecutor{err: failure}
	if err := InitializeForToken(t.Context(), executor, 7, "created", "updated"); !errors.Is(err, failure) || executor.calls != 1 {
		t.Fatalf("initializer lost write failure: %v", err)
	}
}
