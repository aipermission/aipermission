package keymaterial

import (
	"strings"
	"testing"
)

func TestPassphraseSyntaxMatchesPinnedSQLCipherForms(t *testing.T) {
	for _, length := range []int{64, 96, 160} {
		for _, prefix := range []string{"x'", "X'"} {
			value := prefix + strings.Repeat("aA01", length/4) + "'"
			if err := ValidatePassphrase(value); err == nil || strings.Contains(err.Error(), value) {
				t.Fatalf("raw-key syntax accepted or disclosed for length %d", length)
			}
		}
	}
	for _, value := range []string{"", "NormalDatabasePassword123", "x'" + strings.Repeat("aA01", 8) + "'", " x'" + strings.Repeat("aA01", 16) + "'", "x'" + strings.Repeat("aA01", 16) + "' ", "x'" + strings.Repeat("gA01", 16) + "'", "x\"" + strings.Repeat("aA01", 16) + "'", "x'" + strings.Repeat("aA01", 16) + "\"", "y'" + strings.Repeat("aA01", 16) + "'"} {
		if err := ValidatePassphrase(value); err != nil {
			t.Fatalf("ordinary passphrase rejected: %v", err)
		}
	}
}

func TestEscapePreservesSQLCipherQuotedStringIdentity(t *testing.T) {
	if got := EscapeDoubleQuoted(`alpha"bravo";SELECT 1`); got != `alpha""bravo"";SELECT 1` {
		t.Fatalf("escaped string = %q", got)
	}
}
