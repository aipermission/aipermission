package sqlsafe

import (
	"strings"
	"testing"
)

func TestPostgreSQLFunctionCallsRejectMultipartFunctionQualification(t *testing.T) {
	for _, query := range []string{
		`SELECT fixture_db.public.lower(1)`,
		`SELECT fixture_db.pg_catalog.lower('value')`,
		`SELECT fixture_db . public . lower (1)`,
		"SELECT fixture_db\n.\tpublic\r.\flower(1)",
		`SELECT fixture_db/* one */.public/* two */.lower(1)`,
		`SELECT fixture_db.public.extra.lower(1)`,
		`SELECT "fixture_db".public.lower(1)`,
		`SELECT fixture_db."public".lower(1)`,
		`SELECT fixture_db.public."lower"(1)`,
		`SELECT coalesce(fixture_db.public.lower(1), 0)`,
		`EXPLAIN (FORMAT JSON) SELECT fixture_db.public.lower(1)`,
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := PostgreSQLFunctionCalls(query); err == nil || !strings.Contains(err.Error(), "qualification") {
				t.Fatalf("multipart function must be rejected, got %v", err)
			}
		})
	}
}

func TestPostgreSQLFunctionCallsRetainCatalogAndNonfunctionChains(t *testing.T) {
	for _, query := range []string{
		`SELECT fixture_db.public.rows.value, pg_catalog.lower('value') FROM fixture_db.public.rows`,
		`SELECT 'fixture_db.public.lower(1)', pg_catalog.lower('value')`,
		`SELECT $$fixture_db.public.lower(1)$$, pg_catalog.lower('value')`,
		`SELECT pg_catalog/* comment */.lower('value') -- fixture_db.public.lower(1)`,
		`WITH rows(value) AS (SELECT pg_catalog.lower('value')) SELECT rows.value FROM rows`,
	} {
		calls, err := PostgreSQLFunctionCalls(query)
		want := FunctionCall{Schema: "pg_catalog", Name: "lower"}
		if err != nil || len(calls) != 1 || calls[0] != want {
			t.Fatalf("calls for %q = %#v, %v; want %#v", query, calls, err, want)
		}
	}
}
