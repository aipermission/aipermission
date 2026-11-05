package sqlserverconnector

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/antlr4-go/antlr/v4"
	tsql "github.com/bytebase/parser/tsql"
)

func FuzzExecutionSQL(f *testing.F) {
	for _, script := range []string{
		"INSERT INTO dbo.t(v) VALUES(N'\u0131'); UPDATE dbo.t SET v=N'\u65e5\u672c'; DELETE FROM dbo.t;",
		"CREATE TABLE #t(id int);\nGO\nCREATE PROCEDURE dbo.p AS BEGIN PRINT N'GO'; SELECT 1; END;\nGO\nEXEC dbo.p;",
		"/* \u65e5\u672c /* nested */ */ SELECT N'GO\n:r literal', [GO], \"a\"\"b\";\r\nGO -- \u00e7\nSELECT 2;",
		"SELECT 1;\nGO 2\nSELECT 2;", "SELECT 1;\nGO 1", "SELECT 1; GO SELECT 2;", ":r other.sql", "!! echo command", "SELECT $(value);",
		"SELECT 'open", "/* unterminated", "SELECT \xff", "SELECT 1\x00", "-- only comments", "GO\nGO",
		"SELECT " + strings.Repeat("(", 64) + "1" + strings.Repeat(")", 64),
		strings.Repeat("BEGIN ", 300) + "SELECT 1;" + strings.Repeat(" END;", 300),
		"SELECT 1 " + strings.Repeat("/*", 20) + strings.Repeat("*/", 20),
		strings.Repeat("SELECT 1;\nGO\n", 33),
	} {
		f.Add(script)
	}
	f.Fuzz(func(t *testing.T, script string) {
		if len(script) > sqlexecute.MaxSQLBytes+1 {
			return
		}
		batches, err := compileExecutionSQL(script)
		if err != nil {
			return
		}
		if len(batches) < 1 || len(batches) > sqlexecute.MaxBatches {
			t.Fatalf("unbounded batch count: %d", len(batches))
		}
		for _, batch := range batches {
			if !utf8.ValidString(batch) || strings.TrimSpace(batch) == "" {
				t.Fatalf("invalid dispatched bytes: %q", batch)
			}
		}
		assertExecutionByteProvenance(t, script, batches)
		for _, suffix := range []string{"\nGO 1", "\nGO 2"} {
			if _, err := compileExecutionSQL(script + suffix); err == nil {
				t.Fatalf("accepted a GO repeat after %q", script)
			}
		}
	})
}

func assertExecutionByteProvenance(t *testing.T, script string, batches []string) {
	t.Helper()
	position := 0
	for _, batch := range batches {
		offset := strings.Index(script[position:], batch)
		if offset < 0 {
			t.Fatalf("batch is not an exact ordered source slice: %q", batch)
		}
		position += offset + len(batch)
	}
	shadow, offsets, err := executionLexicalInput(script)
	if err != nil {
		t.Fatal(err)
	}
	lexer := tsql.NewTSqlLexer(antlr.NewInputStream(string(shadow)))
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	separators := executionSeparators(shadow, stream.GetAllTokens(), nil)
	var expected strings.Builder
	start := 0
	for _, separator := range separators {
		expected.WriteString(script[start:offsets[separator.GetStart()]])
		start = offsets[separator.GetStop()+1]
	}
	if strings.TrimSpace(script[start:]) != "" {
		expected.WriteString(script[start:])
	}
	if joined := strings.Join(batches, ""); joined != expected.String() || len(joined) > len(script) {
		t.Fatalf("batch splitting invented or rewrote approved SQL: %q -> %#v", script, batches)
	}
}
