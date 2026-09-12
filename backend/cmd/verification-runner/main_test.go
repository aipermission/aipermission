package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyEventsRequiresPassWithoutSkip(t *testing.T) {
	const key = "example/package:TestOne"
	if err := verifyEvents("fixture", `{"Action":"pass","Package":"example/package","Test":"TestOne"}`, []string{key}); err != nil {
		t.Fatal(err)
	}
	if err := verifyEvents("fixture", `{"Action":"skip","Package":"example/package","Test":"TestOne"}`, []string{key}); err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("skip error = %v", err)
	}
	if err := verifyEvents("fixture", `{"Action":"output","Package":"example/package","Test":"TestOne"}`, []string{key}); err == nil || !strings.Contains(err.Error(), "did not pass") {
		t.Fatalf("missing pass error = %v", err)
	}
}

func TestDeclaredTestsUsesGoSyntaxAndRejectsBuildConstraints(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/inventory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	packageDirectory := filepath.Join(root, "fixture")
	if err := os.MkdirAll(packageDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	unconstrained := []byte("package fixture\n\nimport \"testing\"\n\nfunc /* inventory-safe */ FuzzVisible(f *testing.F) {}\n")
	if err := os.WriteFile(filepath.Join(packageDirectory, "fuzz_test.go"), unconstrained, 0o600); err != nil {
		t.Fatal(err)
	}
	discovered, err := declaredTests(root, "Fuzz", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(discovered, ",") != "example.test/inventory/fixture:FuzzVisible" {
		t.Fatalf("declared fuzz inventory = %v", discovered)
	}
	constrained := []byte("//go:build windows\n\npackage fixture\n\nimport \"testing\"\n\nfunc FuzzWindowsOnly(f *testing.F) {}\n")
	if err := os.WriteFile(filepath.Join(packageDirectory, "fuzz_windows_test.go"), constrained, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := declaredTests(root, "Fuzz", true); err == nil || !strings.Contains(err.Error(), "must be portable") {
		t.Fatalf("constrained inventory error = %v", err)
	}
}

func TestInventoryIsStableAndDeduplicatesPackages(t *testing.T) {
	packages, names, err := inventory([]testEntry{
		{Package: "./internal/api", Name: "TestZ"},
		{Package: "./internal/db", Name: "TestA"},
		{Package: "./internal/db", Name: "TestB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(packages, ",") != "./internal/api,./internal/db" {
		t.Fatalf("packages = %v", packages)
	}
	if strings.Join(names, ",") != "github.com/aipermission/aipermission/backend/internal/api:TestZ,github.com/aipermission/aipermission/backend/internal/db:TestA,github.com/aipermission/aipermission/backend/internal/db:TestB" {
		t.Fatalf("names = %v", names)
	}
}

func TestDiscoveryPreservesPackageOwnership(t *testing.T) {
	discovered, err := discoveredTests(
		"{\"Action\":\"output\",\"Package\":\"example/one\",\"Output\":\"TestRecoveryDrillSame\\n\"}\n"+
			"{\"Action\":\"output\",\"Package\":\"example/two\",\"Output\":\"TestRecoveryDrillSame\\n\"}",
		"TestRecoveryDrill",
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(discovered, ",") != "example/one:TestRecoveryDrillSame,example/two:TestRecoveryDrillSame" {
		t.Fatalf("discovered = %v", discovered)
	}
}
