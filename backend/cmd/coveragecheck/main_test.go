package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCoverageProfileAggregatesPackages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.out")
	content := `mode: set
github.com/aipermission/aipermission/backend/internal/api/a.go:1.1,2.1 4 1
github.com/aipermission/aipermission/backend/internal/api/b.go:1.1,2.1 6 0
github.com/aipermission/aipermission/backend/internal/vault/a.go:1.1,2.1 5 2
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	counts, err := readCoverageProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := counts["internal/api"]; got.statements != 10 || got.covered != 4 {
		t.Fatalf("unexpected API coverage: %+v", got)
	}
	if got := counts["internal/vault"]; got.statements != 5 || got.covered != 5 {
		t.Fatalf("unexpected Vault coverage: %+v", got)
	}
}

func TestReadCoverageProfileRejectsMalformedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(path, []byte("not-a-profile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCoverageProfile(path); err == nil {
		t.Fatal("expected malformed profile error")
	}
}
