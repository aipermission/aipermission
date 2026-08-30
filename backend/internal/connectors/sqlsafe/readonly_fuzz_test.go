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
		checkSQL, parseErr := validationSQL(strings.TrimSpace(stripTrailingStatementTerminator(stripLeadingComments(sql))))
		if parseErr != nil {
			t.Fatalf("accepted SQL failed validation scan: %v", parseErr)
		}
		checkSQL = strings.TrimSpace(checkSQL)
		if strings.Contains(checkSQL, ";") || fuzzDisallowedTerms.MatchString(checkSQL) || !hasAllowedPrefix(checkSQL, []string{"select", "with", "show", "explain"}) {
			t.Fatalf("unsafe SQL was accepted: %q normalized=%q", sql, checkSQL)
		}
		if err := ValidateReadOnly(sql+"\n; DROP TABLE fuzz_guard", "query_readonly", 100_000, []string{"select", "with", "show", "explain"}, "SELECT, WITH, SHOW, or EXPLAIN", fuzzDisallowedTerms); err == nil {
			t.Fatalf("appended destructive statement was accepted: %q", sql)
		}
	})
}
