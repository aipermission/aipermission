package api

import "testing"

func TestValidateRemoteBackupPassword(t *testing.T) {
	if err := validateRemoteBackupPassword("M7!river-Quartz_92fox", "My Database"); err != nil {
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
			if err := validateRemoteBackupPassword(test.password, test.database); err == nil {
				t.Fatal("expected password rejection")
			}
		})
	}
}
