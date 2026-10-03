package passwordpolicy

import (
	"strings"
	"testing"
)

func TestRawKeyNotationRequiresChangingToAPassphrase(t *testing.T) {
	for _, length := range []int{64, 96, 160} {
		password := "X'" + strings.Repeat("Ab90cD27ef68", length/12) + strings.Repeat("A", length%12) + "'"
		if err := Validate(password, "Example"); err == nil || !strings.Contains(err.Error(), "raw-key notation") {
			t.Fatalf("raw-key backup rejection = %v", err)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("M7!river-Quartz_92fox", "My Database"); err != nil {
		t.Fatalf("expected strong password: %v", err)
	}
	tests := []struct {
		name     string
		password string
		database string
	}{
		{name: "short", password: "ShortPassword12", database: "Project"},
		{name: "missing class", password: "alllowercase-with-12345", database: "Project"},
		{name: "common term", password: "UniquePassword-48!Fox", database: "Project"},
		{name: "repeated", password: "Strong-AAAA-Value-92", database: "Project"},
		{name: "sequence", password: "Strong-Abcd-Value-92", database: "Project"},
		{name: "database name", password: "MyDatabase-River-92!", database: "My Database"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Validate(test.password, test.database); err == nil {
				t.Fatal("expected password rejection")
			}
		})
	}
}
