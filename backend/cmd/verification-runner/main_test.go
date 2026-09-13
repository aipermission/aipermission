package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("AIPERMISSION_VERIFICATION_RUNNER_HELPER") == "1" {
		mode := os.Args[len(os.Args)-1]
		_, _ = os.Stdout.WriteString("structured-output\n")
		_, _ = os.Stderr.WriteString("diagnostic-output\n")
		if mode == "failure" {
			os.Exit(7)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRunCommandKeepsSuccessfulStderrOutOfStructuredOutput(t *testing.T) {
	command := helperCommand(t, "success")
	output, err := runCommand(command, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if output != "structured-output\n" {
		t.Fatalf("output = %q", output)
	}
}

func TestRunCommandPreservesBothStreamsOnFailure(t *testing.T) {
	command := helperCommand(t, "failure")
	output, err := runCommand(command, "fixture")
	if output != "structured-output\n" {
		t.Fatalf("output = %q", output)
	}
	if err == nil {
		t.Fatal("expected command failure")
	}
	for _, expected := range []string{
		"fixture:",
		"stdout:\nstructured-output",
		"stderr:\ndiagnostic-output",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not contain %q", err, expected)
		}
	}
}

func helperCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestVerificationRunnerHelperProcess$", "--", mode)
	command.Env = append(os.Environ(), "AIPERMISSION_VERIFICATION_RUNNER_HELPER=1")
	return command
}

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
