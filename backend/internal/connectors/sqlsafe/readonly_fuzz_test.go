package sqlsafe

import (
	"regexp"
	"strings"
	"testing"
)

var fuzzDisallowedTerms = regexp.MustCompile(`\b(insert|update|delete|drop|create|alter|truncate|grant|revoke|copy|call|execute|prepare|listen|notify|set|reset|vacuum|analyze|cluster|refresh|reindex|into)\b`)

func FuzzValidateReadOnly(f *testing.F) {
	for _, seed := range []string{
		"SELECT 1",
		"-- read\nWITH values AS (SELECT 1) SELECT * FROM values;",
		"SELECT 'drop table users' AS harmless",
		"SELECT 1; DROP TABLE users",
		"WITH deleted AS (DELETE FROM users RETURNING *) SELECT * FROM deleted",
		"SELECT $$update users$$ AS harmless",
		"WITH safe_rows(id) AS (SELECT 1) SELECT id FROM safe_rows",
		"EXPLAIN (FORMAT JSON, COSTS OFF) SELECT 1",
		"SELECT row_data.id FROM (VALUES (1)) AS row_data(id)",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, sql string) {
		if len(sql) > 64<<10 {
			return
		}
		err := ValidateReadOnly(sql, "query_readonly", 20_000, []string{"select", "with", "show", "explain"}, "SELECT, WITH, SHOW, or EXPLAIN", fuzzDisallowedTerms)
		if err != nil {
			return
		}
		checkSQL, parseErr := validationSQL(sql, DialectANSI)
		if parseErr != nil {
			t.Fatalf("accepted SQL failed validation scan: %v", parseErr)
		}
		checkSQL = strings.TrimSpace(stripTrailingStatementTerminator(checkSQL))
		if strings.Contains(checkSQL, ";") || fuzzDisallowedTerms.MatchString(checkSQL) || !hasAllowedPrefix(checkSQL, []string{"select", "with", "show", "explain"}) {
			t.Fatalf("unsafe SQL was accepted: %q normalized=%q", sql, checkSQL)
		}
		if err := ValidateReadOnly(sql+"\n; DROP TABLE fuzz_guard", "query_readonly", 100_000, []string{"select", "with", "show", "explain"}, "SELECT, WITH, SHOW, or EXPLAIN", fuzzDisallowedTerms); err == nil {
			t.Fatalf("appended destructive statement was accepted: %q", sql)
		}
	})
}

func FuzzPostgreSQLRecordFunctionExtraction(f *testing.F) {
	for _, seed := range []string{"values public.review_domain", "value int", "select text", "with jsonb"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, declaration string) {
		if len(declaration) > 1024 {
			return
		}
		query := `SELECT * FROM json_to_record('{"value":1}') AS (` + declaration + `)`
		calls, err := PostgreSQLFunctionCalls(query)
		if err != nil {
			return
		}
		for _, call := range calls {
			if call.Schema == "" && call.Name == "json_to_record" {
				return
			}
		}
		t.Fatalf("record function disappeared from call inventory: %q => %#v", query, calls)
	})
}
