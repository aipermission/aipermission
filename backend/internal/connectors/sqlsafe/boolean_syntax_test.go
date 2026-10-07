package sqlsafe

import "testing"

func TestPostgreSQLBooleanSyntaxIsNotAFunction(t *testing.T) {
	for _, query := range []string{
		"SELECT NOT (false)", "SELECT true AND (true)", "SELECT false OR (true)",
		"SELECT 1 = ANY (ARRAY[1,2])", "SELECT 1 = SOME (ARRAY[1,2])", "SELECT 1 = ALL (ARRAY[1,1])",
		"SELECT NOT /* group */ (false) OR (true AND (true))",
	} {
		calls, err := PostgreSQLFunctionCalls(query)
		if err != nil || len(calls) != 0 {
			t.Fatalf("%s: calls=%v err=%v", query, calls, err)
		}
	}
}

func TestPostgreSQLBooleanGroupingKeepsNestedAndQualifiedFunctions(t *testing.T) {
	for _, query := range []string{
		"SELECT NOT (pg_notify('x','y') IS NULL)",
		"SELECT true AND (public.and())", "SELECT false OR (public.or())",
		"SELECT 1 = ANY (public.any())", "SELECT 1 = ALL (public.all())",
		`SELECT "not"()`, `SELECT "any"()`,
	} {
		calls, err := PostgreSQLFunctionCalls(query)
		if err != nil || len(calls) != 1 {
			t.Fatalf("hidden function in %s: calls=%v err=%v", query, calls, err)
		}
	}
}
