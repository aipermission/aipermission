package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeasuredProductionOwnerRequiresAnExplicitFloor(t *testing.T) {
	policy := coveragePolicy{defaultFloor: 1, requireExplicitFloor: true}
	packages := productionPackages{"internal/newowner": {"owner.go": {hasStatements: true}}}
	counts := map[string]coverageCount{"internal/newowner": {statements: 10, covered: 10, files: map[string]bool{"owner.go": true}}}
	failures := checkCoverage(policy, counts, packages, io.Discard)
	if len(failures) != 1 || !strings.Contains(failures[0], "no explicit coverage floor") {
		t.Fatalf("measured owner fell back to a placeholder: %v", failures)
	}
	policy.floors = map[string]float64{"internal/newowner": 98}
	if failures := checkCoverage(policy, counts, packages, io.Discard); len(failures) != 0 {
		t.Fatalf("explicit measured baseline rejected: %v", failures)
	}
	counts["internal/newowner"] = coverageCount{statements: 100, covered: 97, files: map[string]bool{"owner.go": true}}
	if failures := checkCoverage(policy, counts, packages, io.Discard); len(failures) != 1 || !strings.Contains(failures[0], "below 98.0% floor") {
		t.Fatalf("explicit baseline regression accepted: %v", failures)
	}
}

func TestReadCoveragePolicyCannotDisableExplicitOwnerFloors(t *testing.T) {
	for _, declaration := range []string{"", `"backendCoverageRequireExplicitFloor":false,`} {
		file := filepath.Join(t.TempDir(), "policy.json")
		content := `{` + declaration + `"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"internal/owner":50}}`
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readCoveragePolicy(file); err == nil || !strings.Contains(err.Error(), "explicit measured owner floors") {
			t.Fatalf("missing or disabled owner-floor requirement accepted: %v", err)
		}
	}
}
