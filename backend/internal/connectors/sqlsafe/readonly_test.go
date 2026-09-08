package sqlsafe

import (
	"regexp"
	"strings"
	"testing"
)

var testDisallowedTerms = regexp.MustCompile(`\b(insert|update|delete|drop|create|alter|into)\b`)

func TestValidateReadOnlyAcceptsOneReadStatement(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1",
		"-- comment\nWITH values AS (SELECT 1) SELECT * FROM values;",
		"SELECT 'drop table users' AS value",
		"SELECT `delete` FROM events",
		"SELECT $$update users$$ AS value",
	} {
		if err := ValidateReadOnly(sql, "query_readonly", 20000, []string{"select", "with", "show", "explain"}, "SELECT, WITH, SHOW, or EXPLAIN", testDisallowedTerms); err != nil {
			t.Fatalf("validate %q: %v", sql, err)
		}
	}
}

func TestValidateReadOnlyRejectsUnsafeSQL(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		message string
	}{
		{name: "empty", sql: " ", message: "sql is required"},
		{name: "write", sql: "UPDATE users SET active = true", message: "read-only"},
		{name: "multi", sql: "SELECT 1; SELECT 2", message: "single statement"},
		{name: "prefix", sql: "DESCRIBE users", message: "only accepts SELECT, WITH, SHOW, or EXPLAIN SQL"},
		{name: "null", sql: "SELECT\x00 1", message: "invalid null byte"},
		{name: "unterminated single quote", sql: "SELECT '", message: "unterminated single-quoted value"},
		{name: "unterminated double quote", sql: `SELECT"`, message: "unterminated quoted identifier"},
		{name: "unterminated backtick quote", sql: "SELECT `", message: "unterminated quoted identifier"},
		{name: "unterminated block comment", sql: "SELECT 1 /*", message: "unterminated block comment"},
		{name: "unterminated dollar quote", sql: "SELECT $$value", message: "unterminated dollar-quoted value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReadOnly(tt.sql, "query_readonly", 20000, []string{"select", "with", "show", "explain"}, "SELECT, WITH, SHOW, or EXPLAIN", testDisallowedTerms)
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("expected %q error, got %v", tt.message, err)
			}
		})
	}
}

func TestValidateReadOnlyEnforcesSizeLimit(t *testing.T) {
	err := ValidateReadOnly("SELECT "+strings.Repeat("x", 20), "query_readonly", 10, []string{"select"}, "SELECT", testDisallowedTerms)
	if err == nil || !strings.Contains(err.Error(), "exceeds 10 bytes") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestPostgreSQLDialectAcceptsEscapeStringsAndNestedComments(t *testing.T) {
	for _, sql := range []string{
		`SELECT E'can\'t; DROP TABLE users' AS value`,
		`/* outer /* inner */ still outer */ SELECT 1`,
	} {
		if err := ValidateReadOnlyDialect(sql, "query_readonly", 20000, []string{"select"}, "SELECT", testDisallowedTerms, DialectPostgreSQL); err != nil {
			t.Fatalf("validate %q: %v", sql, err)
		}
	}
}

func TestPostgreSQLDialectRejectsStatementsAfterEscapeStrings(t *testing.T) {
	err := ValidateReadOnlyDialect(
		`SELECT E'value\\'; DROP TABLE users`,
		"query_readonly",
		20000,
		[]string{"select"},
		"SELECT",
		testDisallowedTerms,
		DialectPostgreSQL,
	)
	if err == nil || !strings.Contains(err.Error(), "single statement") {
		t.Fatalf("expected single statement error, got %v", err)
	}
}

func TestPostgreSQLFunctionCallsIgnoreValuesCommentsAndGrouping(t *testing.T) {
	calls, err := PostgreSQLFunctionCalls(`
		SELECT count(*), pg_catalog.lower(name), (score + 1)
		FROM users
		WHERE note = 'pg_notify()' AND id IN (SELECT id FROM active_users)
		-- dblink_exec()
	`)
	if err != nil {
		t.Fatalf("function calls: %v", err)
	}
	want := []FunctionCall{{Name: "count"}, {Schema: "pg_catalog", Name: "lower"}}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("call[%d] = %#v, want %#v", index, calls[index], want[index])
		}
	}
}

func TestPostgreSQLFunctionCallsExposeQuotedIdentifiers(t *testing.T) {
	calls, err := PostgreSQLFunctionCalls(`SELECT public."side effect"(), "other"()`)
	if err != nil {
		t.Fatalf("function calls: %v", err)
	}
	want := []FunctionCall{
		{Schema: "public", Name: "quoted_identifier"},
		{Name: "quoted_identifier"},
	}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestPostgreSQLFunctionCallsExposeNonASCIIIdentifiers(t *testing.T) {
	calls, err := PostgreSQLFunctionCalls(`SELECT şüpheli()`)
	if err != nil {
		t.Fatalf("function calls: %v", err)
	}
	if len(calls) != 1 || calls[0].Name != "şüpheli" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestValidatePostgreSQLResolutionSyntaxRejectsExplicitOperatorsAndCasts(t *testing.T) {
	for _, query := range []string{
		`SELECT left_value OPERATOR(public.===) right_value FROM public.records`,
		`SELECT payload::public.side_effect_type FROM public.records`,
		`SELECT CAST(payload AS public.side_effect_type) FROM public.records`,
	} {
		if err := ValidatePostgreSQLResolutionSyntax(query); err == nil {
			t.Fatalf("unsafe resolution syntax accepted: %q", query)
		}
	}
	for _, query := range []string{
		`SELECT id = 1 FROM public.records`,
		`SELECT 'operator(public.===)', '::public.type', 'cast(value as text)'`,
		`SELECT 1 /* OPERATOR(public.===) */`,
	} {
		if err := ValidatePostgreSQLResolutionSyntax(query); err != nil {
			t.Fatalf("safe query rejected: %q: %v", query, err)
		}
	}
}
