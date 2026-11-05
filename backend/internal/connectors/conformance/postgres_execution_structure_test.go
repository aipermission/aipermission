package conformance_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
)

func testPostgresSQLExecutionStructure(t *testing.T) {
	for _, probe := range []struct{ name, query string }{
		{"nodes", "SELECT array_fill('-0'::float8, ARRAY[19000]) AS a FROM generate_series(1,8)"},
		{"depth", "SELECT '" + strings.Repeat("[", 30) + "1" + strings.Repeat("]", 30) + "'::jsonb AS a"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			f := newPostgresExecutionFixture(t)
			script := fmt.Sprintf("UPDATE %s SET value=1; %s; UPDATE %s SET value=value+10;", f.table, probe.query, f.table)
			result := executeAction(t, f.connector, f.runtime, sqlexecute.Action, map[string]any{"sql": script})
			redactor, err := actionresult.NewRedactor(func(_ context.Context, s string) string { return s }, func(_ context.Context, s string) string { return s }, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			projected, err := redactor.ResultWithCredentialBoundary(t.Context(), result, actionresult.CredentialBoundary{}, sqlexecute.Definition("SQL").OutputHint)
			if err != nil || projected.Status != connectors.ResultCompleted {
				t.Fatalf("completed mutation became uncertain: %#v %v", projected, err)
			}
			out := decodeSQLExecution(t, projected)
			if !out.Truncated || out.CompletedBatches != 1 || f.value(t) != 11 || len(out.Commands) != 3 {
				t.Fatalf("structural retention interrupted mutation or lost acknowledgements: %#v", out)
			}
		})
	}
}
