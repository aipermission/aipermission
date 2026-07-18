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
