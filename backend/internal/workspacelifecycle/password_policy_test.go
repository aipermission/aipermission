package workspacelifecycle

import (
	"strings"
	"testing"
)

func TestPasswordPolicyCountsUnicodeCodePoints(t *testing.T) {
	for _, test := range []struct {
		name, password string
		valid          bool
	}{
		{"ASCII below minimum", "Aa1" + strings.Repeat("x", 10), false},
		{"ASCII minimum", "Aa1" + strings.Repeat("x", 11), true},
		{"multibyte below minimum", "Aa1" + strings.Repeat("\u00e9", 10), false},
		{"multibyte minimum", "Aa1" + strings.Repeat("\u00e9", 11), true},
		{"astral below minimum", "Aa1" + strings.Repeat("\U0001f680", 10), false},
		{"astral minimum", "Aa1" + strings.Repeat("\U0001f680", 11), true},
		{"combining below minimum", "Aa1" + strings.Repeat("e\u0301", 5), false},
		{"combining minimum", "Aa1" + strings.Repeat("e\u0301", 5) + "x", true},
		{"ASCII uppercase required", "aa1" + strings.Repeat("\u00c9", 11), false},
		{"ASCII digit required", "Aa\u0661" + strings.Repeat("x", 11), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidatePassword(test.password, test.password) == nil; got != test.valid {
				t.Fatalf("valid=%t, want %t", got, test.valid)
			}
		})
	}
}
