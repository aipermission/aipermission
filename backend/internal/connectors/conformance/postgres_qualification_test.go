package conformance_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
)

func TestPostgresMultipartFunctionQualificationRealService(t *testing.T) {
	requireConformance(t)
	assertMultipartFunctionQualificationRejected(t, postgresconnector.New(), postgresConformanceRuntime(t))
}

func assertMultipartFunctionQualificationRejected(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	conn := connectPostgresPolicyFixture(t)
	defer conn.Close(context.Background())
	if _, err := conn.Exec(t.Context(), `CREATE OR REPLACE FUNCTION public.lower(integer) RETURNS integer LANGUAGE SQL AS 'SELECT 47'`); err != nil {
		t.Fatalf("create multipart qualification fixture: %v", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `DROP FUNCTION IF EXISTS public.lower(integer)`)
	}()
	for _, query := range []string{
		`SELECT aipermission.public.lower(1) AS result`,
		`SELECT aipermission /* database */ . public /* schema */ . lower(1) AS result`,
		`SELECT "aipermission".public.lower(1) AS result`,
	} {
		var result int
		if err := conn.QueryRow(t.Context(), query).Scan(&result); err != nil || result != 47 {
			t.Fatalf("real-server qualification control result=%d err=%v", result, err)
		}
		prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{
			Source: "conformance", Target: runtime.Target, Profile: runtime.Profile,
			ActionName: postgresconnector.ActionQueryReadonly,
			Input:      map[string]any{"sql": query, "max_rows": 5}, Reason: "reject custom qualified function",
		})
		if err == nil || !strings.Contains(err.Error(), "function qualification") {
			t.Fatalf("custom qualified function admitted: prepared=%#v err=%v", prepared, err)
		}
		_, err = connector.ExecuteAction(t.Context(), runtime, connectors.PreparedAction{
			ActionName: postgresconnector.ActionQueryReadonly,
			Payload:    map[string]any{"sql": query, "max_rows": 5},
		})
		if err == nil || !strings.Contains(err.Error(), "function qualification") {
			t.Fatalf("runtime revalidation admitted qualified function: %v", err)
		}
	}
	result := executeAction(t, connector, runtime, postgresconnector.ActionQueryReadonly, map[string]any{
		"sql": `SELECT pg_catalog.lower('QUALIFICATION-CONTROL') AS marker`, "max_rows": 5,
	})
	assertResultContains(t, result, "qualification-control")
}
