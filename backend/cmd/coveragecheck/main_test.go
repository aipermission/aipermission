package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadCoverageProfileAggregatesPackages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.out")
	content := `mode: count
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
	fixtures := []string{
		"not-a-profile\n",
		"mode: nonsense\nbackend/internal/api/a.go:1.1,2.1 1 1\n",
		"mode: set\n",
		"mode: set\nbackend/internal/api/a.go:1.1,2.1 1 2\n",
		"mode: set\nelsewhere/internal/api/a.go:1.1,2.1 1 1\n",
		"mode: set\nbackend/internal/api/a.go:not-a-position 1 1\n",
		"mode: set\nbackend/internal/api/a.go:2.2,1.1 1 1\n",
	}
	for _, content := range fixtures {
		path := filepath.Join(t.TempDir(), "coverage.out")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readCoverageProfile(path); err == nil {
			t.Fatalf("expected malformed profile error for %q", content)
		}
	}
}

func TestCheckCoverageRejectsMissingAndUnknownProductionMeasurements(t *testing.T) {
	policy := coveragePolicy{
		floors:          map[string]float64{"internal/api": 50},
		defaultFloor:    10,
		neutralPackages: map[string]bool{"internal/contracts": true},
	}
	packages := productionPackages{
		"internal/api":       {"api.go": {hasStatements: true}},
		"internal/contracts": {"contracts.go": {}},
		"internal/newowner":  {"new.go": {hasStatements: true}},
	}
	counts := map[string]coverageCount{
		"internal/api":       {statements: 10, covered: 5, files: map[string]bool{"api.go": true, "missing.go": true}},
		"internal/contracts": {statements: 1},
		"internal/imaginary": {statements: 1, covered: 1, files: map[string]bool{"imaginary.go": true}},
		"internal/newowner":  {files: map[string]bool{"new.go": true}},
	}
	var output bytes.Buffer
	failures := checkCoverage(policy, counts, packages, &output)
	joined := strings.Join(failures, "\n")
	for _, expected := range []string{
		"internal/api/missing.go: coverage profile contains a file outside the production build",
		"internal/imaginary: coverage profile contains a package outside the production build",
		"internal/newowner: no coverage statements found",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing failure %q in %q", expected, joined)
		}
	}
	if !strings.Contains(output.String(), "internal/api") {
		t.Fatalf("expected measured package output, got %q", output.String())
	}
}

func TestCheckCoverageRejectsUnmeasuredExecutableFilesAndBehavioralNeutralPackages(t *testing.T) {
	policy := coveragePolicy{
		floors:          map[string]float64{"internal/api": 50},
		defaultFloor:    10,
		neutralPackages: map[string]bool{"internal/contracts": true},
	}
	packages := productionPackages{
		"internal/api": {
			"measured.go": {hasStatements: true},
			"missing.go":  {hasStatements: true},
			"types.go":    {},
		},
		"internal/contracts": {
			"contracts.go": {hasStatements: true},
		},
	}
	counts := map[string]coverageCount{
		"internal/api": {statements: 10, covered: 5, files: map[string]bool{"measured.go": true}},
	}
	var output bytes.Buffer
	failures := strings.Join(checkCoverage(policy, counts, packages, &output), "\n")
	for _, expected := range []string{
		"internal/api/missing.go: executable production file has no coverage measurements",
		"internal/contracts/contracts.go: configured neutral package contains executable statements",
	} {
		if !strings.Contains(failures, expected) {
			t.Fatalf("missing failure %q in %q", expected, failures)
		}
	}
	if strings.Contains(failures, "types.go") {
		t.Fatalf("declaration-only source should not require profile measurements: %q", failures)
	}
}

func TestReadProductionPackagesUsesActiveGoAndCgoSources(t *testing.T) {
	input := strings.NewReader(`
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/api","GoFiles":["server.go"]}
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/crypto","CgoFiles":["cipher.go"]}
`)
	packages, err := readProductionPackages(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := packages["internal/api"]["server.go"]; !ok {
		t.Fatalf("missing active Go source: %#v", packages)
	}
	if _, ok := packages["internal/crypto"]["cipher.go"]; !ok || len(packages) != 2 {
		t.Fatalf("unexpected package inventory: %#v", packages)
	}
}

func TestReadProductionPackagesClassifiesExecutableSources(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "types.go"), []byte("package sample\ntype Value string\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "runtime.go"), []byte("package sample\nfunc Run() { println(\"run\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "noop.go"), []byte("package sample\nfunc Noop() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "initializer.go"), []byte("package sample\nimport \"strings\"\nvar Value = strings.TrimSpace(\" value \" )\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader(`{"ImportPath":"github.com/aipermission/aipermission/backend/internal/sample","Dir":` + strconv.Quote(directory) + `,"GoFiles":["types.go","runtime.go","noop.go","initializer.go"]}`)
	packages, err := readProductionPackages(input)
	if err != nil {
		t.Fatal(err)
	}
	if packages["internal/sample"]["types.go"].hasStatements {
		t.Fatal("declaration-only source classified as executable")
	}
	if !packages["internal/sample"]["runtime.go"].hasStatements {
		t.Fatal("function body was not classified as executable")
	}
	if !packages["internal/sample"]["noop.go"].hasStatements {
		t.Fatal("empty function body was not classified as executable")
	}
	if !packages["internal/sample"]["initializer.go"].hasStatements {
		t.Fatal("package initializer call was not classified as executable")
	}
}

func TestReadProductionPackagesRejectsEmptyAndExternalEntries(t *testing.T) {
	for _, input := range []string{
		"",
		`{"ImportPath":"example.com/external","GoFiles":["main.go"]}`,
		`{"ImportPath":"github.com/aipermission/aipermission/backend/internal/empty"}`,
		`{"ImportPath":"github.com/aipermission/aipermission/backend/internal/api","GoFiles":["one.go"]}
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/api","GoFiles":["two.go"]}`,
		"not-json",
	} {
		if _, err := readProductionPackages(strings.NewReader(input)); err == nil {
			t.Fatalf("expected invalid inventory to fail: %q", input)
		}
	}
}

func TestReadCoverageFloorsUsesCanonicalPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance-policy.json")
	if err := os.WriteFile(path, []byte(`{"backendCoverageDefaultFloor":1,"backendCoverageNeutralPackages":["internal/data"],"backendCoverageFloors":{"internal/gatewayworkspace":7}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := readCoveragePolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if policy.floors["internal/gatewayworkspace"] != 7 || policy.defaultFloor != 1 || !policy.neutralPackages["internal/data"] {
		t.Fatalf("unexpected gateway workspace policy: %+v", policy)
	}
}

func TestReadCoverageFloorsRejectsMissingAndInvalidEntries(t *testing.T) {
	for _, content := range []string{
		`{}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"internal/gatewayworkspace":0}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"internal/gatewayworkspace":0.001}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"internal/gatewayworkspace":101}}`,
		`{"backendCoverageDefaultFloor":0,"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageNeutralPackages":["internal/gatewayworkspace"],"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
	} {
		path := filepath.Join(t.TempDir(), "maintenance-policy.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readCoveragePolicy(path); err == nil {
			t.Fatalf("expected invalid policy to fail: %s", content)
		}
	}
}
