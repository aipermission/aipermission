package sqlserverconnector

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestReadonlyPolicyRejectsDifferentialCorpusWithoutPanic(t *testing.T) {
	data, err := os.ReadFile("testdata/readonly-attacks.json")
	if err != nil {
		t.Fatal(err)
	}
	var queries []string
	if err := json.Unmarshal(data, &queries); err != nil {
		t.Fatal(err)
	}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			if err := validateReadonlySQL(query); err == nil {
				t.Fatal("unsafe query accepted")
			}
		})
	}
}

func TestReadonlyTokenProjectionPreservesDataAndQuotedNames(t *testing.T) {
	for _, query := range []string{
		"SELECT 'a''b; DELETE dbo.x' AS [message]",
		"SELECT N'雪''b' AS [a]]b]",
		"SELECT *\nFROM [dbo].[it's] WHERE [name] = 'hello'",
		"SELECT *\r\nFROM [dbo].[order details] WHERE [name]='hello'",
		"SELECT 1 AS [SHUTDOWN] -- ordinary comment\n",
		"SELECT CAST('text' AS nvarchar(MAX)) AS [text]",
		"SELECT DATEADD(Day, 1, CAST('2026-01-01' AS date)) AS [next_day]",
		"SELECT 1., 1.e2, 0x, 0xF, $1.00",
		"SELECT \"a\"\"b\" FROM dbo.t",
		"SELECT value FROM dbo.t",
		"SELECT * FROM dbo.t ORDER BY id OFFSET 0 ROWS FETCH FIRST 1 ROW ONLY",
	} {
		compiled, err := compileReadonlySQL(query)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		if strings.Contains(compiled, "--") {
			t.Fatalf("raw SQL was not projected: %s", compiled)
		}
		if _, err := compileReadonlySQL(compiled); err != nil {
			t.Fatalf("compiled SQL is not accepted: %s: %v", compiled, err)
		}
	}
	for _, query := range []string{"SELECT 'unterminated", "SELECT [unterminated", "SELECT 1\v", "SELECT 1 FETCH c", "SELECT 1 /* unclosed"} {
		if _, err := compileReadonlySQL(query); err == nil {
			t.Fatalf("ambiguous input accepted: %q", query)
		}
	}
}

func TestReadonlyProjectionPreservesQuotedIdentifierMeaning(t *testing.T) {
	for query, expected := range map[string]string{
		`SELECT "a""b" FROM dbo.t`:               `[a"b]`,
		`SELECT "a]b" FROM dbo.t`:                `[a]]b]`,
		`SELECT [a]]b] FROM dbo.t`:               `[a]]b]`,
		`SELECT 'a''b; DELETE dbo.x' AS message`: `'a''b; DELETE dbo.x'`,
		`SELECT 1.e2`:                            `1.e2`,
		`SELECT vAlue`:                           `[vAlue]`,
		`SELECT [vAlue]`:                         `[vAlue]`,
	} {
		compiled, err := compileReadonlySQL(query)
		if err != nil || !strings.Contains(compiled, expected) {
			t.Fatalf("meaning changed: %q -> %q: %v", query, compiled, err)
		}
	}
	for _, depth := range []int{17, 512, 1000} {
		query := "SELECT 1 " + strings.Repeat("/*", depth) + strings.Repeat("*/", depth)
		if _, err := compileReadonlySQL(query); err == nil {
			t.Fatalf("accepted excessive comment nesting %d", depth)
		}
	}
	if _, err := compileReadonlySQL("SELECT 1 /* outer /* inner */ end */"); err != nil {
		t.Fatal(err)
	}
}
