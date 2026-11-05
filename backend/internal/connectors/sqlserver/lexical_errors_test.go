package sqlserverconnector

import (
	"strings"
	"testing"
)

func TestSQLLexicalErrorsNameTheRequestedAction(t *testing.T) {
	for _, sql := range []string{"SELECT 'open", "SELECT [open", "SELECT 1; /* open", "SELECT 1; " + strings.Repeat("/*", 17)} {
		_, readErr := compileReadonlySQL(sql)
		_, writeErr := compileExecutionSQL(t.Context(), sql)
		if readErr == nil || !strings.Contains(readErr.Error(), "query_readonly") {
			t.Fatalf("read action context missing: %v", readErr)
		}
		if writeErr == nil || !strings.Contains(writeErr.Error(), "execute_sql") || strings.Contains(writeErr.Error(), "query_readonly") {
			t.Fatalf("write action context incorrect: %v", writeErr)
		}
	}
}
