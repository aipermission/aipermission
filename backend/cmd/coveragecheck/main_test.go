package main

import (
	"bytes"
	"os"
	"path/filepath"
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
	packages := map[string]map[string]bool{
		"internal/api":       {"api.go": true},
		"internal/contracts": {"contracts.go": true},
		"internal/newowner":  {"new.go": true},
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

func TestReadProductionPackagesUsesActiveGoAndCgoSources(t *testing.T) {
	input := strings.NewReader(`
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/api","GoFiles":["server.go"]}
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/crypto","CgoFiles":["cipher.go"]}
`)
	packages, err := readProductionPackages(input)
	if err != nil {
		t.Fatal(err)
	}
	if !packages["internal/api"]["server.go"] || !packages["internal/crypto"]["cipher.go"] || len(packages) != 2 {
		t.Fatalf("unexpected package inventory: %#v", packages)
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
