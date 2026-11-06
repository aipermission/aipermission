package sqlsafe

import "testing"

func TestPostgresLexicalFunctionBoundaries(t *testing.T) {
	for _, separator := range []string{" ", "\t", "\n", "\r", "\f", "\v"} {
		calls, err := PostgreSQLFunctionCalls("SELECT pg_catalog.pg_read_file" + separator + "('fixture')")
		if err != nil || len(calls) != 1 || calls[0] != (FunctionCall{Schema: "pg_catalog", Name: "pg_read_file"}) {
			t.Fatalf("separator %q: calls=%#v err=%v", separator, calls, err)
		}
	}
	for _, ending := range []string{"\n", "\r", "\r\n"} {
		calls, err := PostgreSQLFunctionCalls("SELECT 1--comment" + ending + ", pg_read_file('fixture')")
		if err != nil || len(calls) != 1 || calls[0].Name != "pg_read_file" {
			t.Fatalf("comment ending %q: calls=%#v err=%v", ending, calls, err)
		}
	}
}

func TestPostgresLexicalResolutionWhitespace(t *testing.T) {
	for _, separator := range []string{" ", "\t", "\n", "\r", "\f", "\v"} {
		for _, query := range []string{
			"SELECT 1 OPERATOR" + separator + "(pg_catalog.+) 1",
			"SELECT CAST" + separator + "('1' AS int)",
			"SELECT public.mytype" + separator + "'x'",
		} {
			if err := ValidatePostgreSQLResolutionSyntax(query); err == nil {
				t.Fatalf("unsafe resolution syntax accepted: %q", query)
			}
		}
	}
}
