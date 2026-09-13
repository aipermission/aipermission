package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
{"ImportPath":"fmt","GoFiles":["print.go"]}
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/api","GoFiles":["server.go"]}
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/crypto","CgoFiles":["cipher.go"]}
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/_hidden","GoFiles":["hidden.go"]}
{"ImportPath":"github.com/aipermission/aipermission/backend/cmd/openapi","GoFiles":["main.go"]}
`)
	packages, err := readProductionPackages(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := packages["internal/api"]["server.go"]; !ok {
		t.Fatalf("missing active Go source: %#v", packages)
	}
	if _, ok := packages["internal/crypto"]["cipher.go"]; !ok || len(packages) != 4 {
		t.Fatalf("unexpected package inventory: %#v", packages)
	}
	if _, ok := packages["internal/_hidden"]["hidden.go"]; !ok {
		t.Fatalf("missing imported underscore-directory source: %#v", packages)
	}
	if _, ok := packages["cmd/openapi"]["main.go"]; !ok {
		t.Fatalf("missing command source: %#v", packages)
	}
}

func TestInventoryEnvironmentReplacesGoBuildSelectors(t *testing.T) {
	got := inventoryEnvironment([]string{
		"PATH=/bin",
		"GOOS=linux",
		"GOARCH=arm64",
		"CGO_ENABLED=1",
	}, buildContext{goos: "windows", goarch: "amd64", cgo: "0"})
	want := []string{"PATH=/bin", "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}

func TestProductionInventoryIncludesNativeAndTaggedBuildContexts(t *testing.T) {
	contexts := productionBuildContexts()
	if len(contexts) != 3 {
		t.Fatalf("build contexts = %#v", contexts)
	}
	if got := inventoryArguments(contexts[1]); strings.Contains(strings.Join(got, " "), "-tags=") {
		t.Fatalf("Windows inventory unexpectedly has build tags: %v", got)
	}
	if got := inventoryArguments(contexts[0]); !strings.Contains(strings.Join(got, " "), "-deps") {
		t.Fatalf("production inventory must include module-local dependencies: %v", got)
	}
	if got := inventoryArguments(contexts[2]); !strings.Contains(strings.Join(got, " "), "-tags=e2e") {
		t.Fatalf("tagged command inventory is missing e2e context: %v", got)
	}
}

func TestCompleteProductionSourceInventoryRejectsUnknownBuildContexts(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "internal", "example")
	if err := os.MkdirAll(filepath.Join(packageDir, "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"active.go", "platform_darwin.go", "ignored_test.go", "testdata/fixture.go"} {
		path := filepath.Join(packageDir, filepath.FromSlash(source))
		if err := os.WriteFile(path, []byte("package example\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inventory := productionPackages{
		"internal/example": {"active.go": {contexts: map[string]bool{"host": true}}},
	}
	activePath := filepath.Join(packageDir, "active.go")
	if err := os.WriteFile(activePath, []byte("package example\nimport _ \"example.invalid/project/testdata\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := requireCompleteProductionSourceInventory(root, inventory)
	if err == nil || !strings.Contains(err.Error(), "imports test-only package") {
		t.Fatalf("testdata import error = %v", err)
	}
	if err := os.WriteFile(activePath, []byte("package example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = requireCompleteProductionSourceInventory(root, inventory)
	if err == nil || !strings.Contains(err.Error(), "internal/example/platform_darwin.go") {
		t.Fatalf("unknown platform source error = %v", err)
	}

	inventory["internal/example"]["platform_darwin.go"] = productionFile{contexts: map[string]bool{"darwin": true}}
	if err := requireCompleteProductionSourceInventory(root, inventory); err != nil {
		t.Fatalf("represented platform source rejected: %v", err)
	}

	link := filepath.Join(root, "internal", "_disguised")
	if err := os.Symlink(packageDir, link); err != nil {
		t.Skipf("cannot create symlink fixture: %v", err)
	}
	if err := requireCompleteProductionSourceInventory(root, inventory); err == nil || !strings.Contains(err.Error(), "contains symlink") {
		t.Fatalf("source symlink error = %v", err)
	}
}

func TestCompleteProductionSourceInventoryScansRepresentedUnderscoreDirectory(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "internal", "_explicit")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "unseen.go"), []byte("package explicit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory := productionPackages{"internal/_explicit": {}}
	err := requireCompleteProductionSourceInventory(root, inventory)
	if err == nil || !strings.Contains(err.Error(), "internal/_explicit/unseen.go") {
		t.Fatalf("represented underscore directory error = %v", err)
	}
}

func TestCheckCoverageRequiresExplicitPlatformSourceEvidence(t *testing.T) {
	packages := productionPackages{
		"internal/db": {
			"database.go":          {hasStatements: true, contexts: map[string]bool{"host": true}},
			"ownership_windows.go": {hasStatements: true, contexts: map[string]bool{"windows": true}, buildConstraint: "windows"},
		},
	}
	counts := map[string]coverageCount{
		"internal/db": {statements: 10, covered: 10, files: map[string]bool{"database.go": true}},
	}
	policy := coveragePolicy{defaultFloor: 1, neutralPackages: map[string]bool{}, platformFiles: map[string]platformCoverageEvidence{}}
	failures := strings.Join(checkCoverage(policy, counts, packages, &bytes.Buffer{}), "\n")
	if !strings.Contains(failures, "ownership_windows.go: executable production file has no coverage measurements") {
		t.Fatalf("missing platform source failure: %s", failures)
	}

	policy.platformFiles["internal/db/ownership_windows.go"] = platformCoverageEvidence{
		Platform: "windows", BuildConstraint: "windows", MinimumCoverage: 100,
		Tests: []runtimeTestEvidence{{Package: "example/backend/internal/db", Name: "TestOwnership"}},
	}
	if failures := checkCoverage(policy, counts, packages, &bytes.Buffer{}); len(failures) != 0 {
		t.Fatalf("explicit platform evidence failures = %v", failures)
	}
	packages["internal/db"]["ownership_windows.go"] = productionFile{hasStatements: true, contexts: map[string]bool{"host": true, "windows": true}, buildConstraint: "windows"}
	if failures := checkCoverage(policy, counts, packages, &bytes.Buffer{}); len(failures) != 0 {
		t.Fatalf("host alias must not invalidate exact source constraint: %v", failures)
	}
	packages["internal/db"]["ownership_windows.go"] = productionFile{hasStatements: true, contexts: map[string]bool{"windows": true}, buildConstraint: "!linux"}
	if failures := strings.Join(checkCoverage(policy, counts, packages, &bytes.Buffer{}), "\n"); !strings.Contains(failures, "source uses \"!linux\"") {
		t.Fatalf("missing build-constraint mismatch: %s", failures)
	}
	packages["internal/db"]["ownership_windows.go"] = productionFile{hasStatements: true, contexts: map[string]bool{"windows": true}, buildConstraint: "windows"}
	counts["internal/db"] = coverageCount{statements: 12, covered: 12, files: map[string]bool{"database.go": true, "ownership_windows.go": true}}
	if failures := checkCoverage(policy, counts, packages, &bytes.Buffer{}); len(failures) != 0 {
		t.Fatalf("measured platform source must remain valid: %v", failures)
	}
}

func TestCheckCoverageAllowsPlatformOnlyExecutablePackage(t *testing.T) {
	packages := productionPackages{
		"internal/windowsruntime": {
			"runtime_windows.go": {hasStatements: true, contexts: map[string]bool{"windows": true}, buildConstraint: "windows"},
		},
	}
	policy := coveragePolicy{
		defaultFloor:    1,
		neutralPackages: map[string]bool{},
		platformFiles: map[string]platformCoverageEvidence{
			"internal/windowsruntime/runtime_windows.go": {
				Platform: "windows", BuildConstraint: "windows", MinimumCoverage: 100,
				Tests: []runtimeTestEvidence{{Package: "example/backend/internal/windowsruntime", Name: "TestRuntime"}},
			},
		},
	}
	if failures := checkCoverage(policy, nil, packages, &bytes.Buffer{}); len(failures) != 0 {
		t.Fatalf("platform-only package failures = %v", failures)
	}
}

func TestCheckCoverageAllowsOnlyExistingExecutableExcludedCommands(t *testing.T) {
	packages := productionPackages{
		"internal/api": {"api.go": {hasStatements: true, contexts: map[string]bool{"host": true}}},
		"cmd/e2e":      {"main.go": {hasStatements: true, contexts: map[string]bool{"linux-e2e": true}}},
	}
	counts := map[string]coverageCount{
		"internal/api": {statements: 1, covered: 1, files: map[string]bool{"api.go": true}},
	}
	policy := coveragePolicy{defaultFloor: 1, excludedPackages: map[string]coverageExclusion{"cmd/e2e": {Context: "linux-e2e", Reason: "test harness"}}}
	if failures := checkCoverage(policy, counts, packages, &bytes.Buffer{}); len(failures) != 0 {
		t.Fatalf("test-only command exclusion failures = %v", failures)
	}
	policy.excludedPackages["cmd/missing"] = coverageExclusion{Context: "linux-e2e", Reason: "test harness"}
	if failures := strings.Join(checkCoverage(policy, counts, packages, &bytes.Buffer{}), "\n"); !strings.Contains(failures, "configured coverage exclusion is not executable production code") {
		t.Fatalf("missing exclusion failure: %s", failures)
	}
	delete(policy.excludedPackages, "cmd/missing")
	packages["cmd/e2e"]["main.go"] = productionFile{hasStatements: true, contexts: map[string]bool{"host": true, "linux-e2e": true}}
	if failures := strings.Join(checkCoverage(policy, counts, packages, &bytes.Buffer{}), "\n"); !strings.Contains(failures, "must exist only in declared linux-e2e build context") {
		t.Fatalf("missing cross-context exclusion failure: %s", failures)
	}
}

func TestMergeProductionPackagesIncludesPlatformSpecificFiles(t *testing.T) {
	merged := mergeProductionPackages(map[string]productionPackages{
		"host":    {"internal/db": {"database.go": {hasStatements: true}}},
		"windows": {"internal/db": {"database.go": {hasStatements: true}, "ownership_windows.go": {hasStatements: true}}},
	})
	if len(merged["internal/db"]) != 2 || !merged["internal/db"]["ownership_windows.go"].hasStatements {
		t.Fatalf("merged inventory = %#v", merged)
	}
	if !merged["internal/db"]["database.go"].contexts["host"] || !merged["internal/db"]["database.go"].contexts["windows"] {
		t.Fatalf("merged inventory lost build contexts: %#v", merged)
	}
}

func TestProductionFileBuildConstraintUsesCanonicalExpression(t *testing.T) {
	directory := t.TempDir()
	constrained := filepath.Join(directory, "platform.go")
	if err := os.WriteFile(constrained, []byte("//go:build (windows || darwin) && amd64\n\npackage sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := productionFileBuildConstraint(constrained)
	if err != nil {
		t.Fatal(err)
	}
	if got != "(windows || darwin) && amd64" {
		t.Fatalf("build constraint = %q", got)
	}
	plain := filepath.Join(directory, "plain.go")
	if err := os.WriteFile(plain, []byte("package sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := productionFileBuildConstraint(plain); err != nil || got != "" {
		t.Fatalf("plain build constraint = %q, %v", got, err)
	}
	blockComment := filepath.Join(directory, "block_comment.go")
	if err := os.WriteFile(blockComment, []byte("/*\n//go:build windows\n*/\npackage sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := productionFileBuildConstraint(blockComment); err != nil || got != "" {
		t.Fatalf("block-comment build constraint = %q, %v", got, err)
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
		`{"ImportPath":"github.com/aipermission/aipermission/backend/internal/example/testdata","GoFiles":["fixture.go"]}`,
		`{"ImportPath":"github.com/aipermission/aipermission/backend/internal/api","GoFiles":["one.go"]}
{"ImportPath":"github.com/aipermission/aipermission/backend/internal/api","GoFiles":["two.go"]}`,
		"not-json",
	} {
		if _, err := readProductionPackages(strings.NewReader(input)); err == nil {
			t.Fatalf("expected invalid inventory to fail: %q", input)
		}
	}
}

func TestReadProductionPackagesBindsImportPathsToRepositoryDirectories(t *testing.T) {
	testRoot := t.TempDir()
	backendRoot := filepath.Join(testRoot, "repository", "backend")
	localAPI := filepath.Join(backendRoot, "internal", "api")
	localExternal := filepath.Join(testRoot, "repository", "shared")
	externalRoot := filepath.Join(testRoot, "external")
	for _, directory := range []string{localAPI, localExternal, externalRoot} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "source.go"), []byte("package fixture\nfunc Run() {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	localExternalInput := fmt.Sprintf(
		`{"ImportPath":"example.invalid/nested","Dir":%q,"GoFiles":["source.go"]}`,
		localExternal,
	)
	if _, err := readProductionPackagesAt(strings.NewReader(localExternalInput), backendRoot); err == nil || !strings.Contains(err.Error(), "repository-local production dependency") {
		t.Fatalf("repository-local external import error = %v", err)
	}

	externalBackendInput := fmt.Sprintf(
		`{"ImportPath":%q,"Dir":%q,"GoFiles":["source.go"]}`,
		backendModulePath+"/internal/api",
		externalRoot,
	)
	if _, err := readProductionPackagesAt(strings.NewReader(externalBackendInput), backendRoot); err == nil || !strings.Contains(err.Error(), "resolves outside the repository") {
		t.Fatalf("external backend import error = %v", err)
	}

	validInput := fmt.Sprintf(
		"{\"ImportPath\":%q,\"Dir\":%q,\"GoFiles\":[\"source.go\"]}\n{\"ImportPath\":\"example.invalid/dependency\",\"Dir\":%q,\"GoFiles\":[\"source.go\"]}",
		backendModulePath+"/internal/api",
		localAPI,
		externalRoot,
	)
	packages, err := readProductionPackagesAt(strings.NewReader(validInput), backendRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages["internal/api"]["source.go"].hasStatements != true {
		t.Fatalf("unexpected repository-bound inventory: %#v", packages)
	}
}

func TestReadCoverageFloorsUsesCanonicalPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance-policy.json")
	if err := os.WriteFile(path, []byte(`{"backendCoverageDefaultFloor":1,"backendCoverageNeutralPackages":["internal/data"],"backendCoverageExcludedPackages":{"cmd/e2e":{"context":"linux-e2e","reason":"test harness"}},"backendCoveragePlatformFiles":{"internal/db/ownership_windows.go":{"platform":"windows","buildConstraint":"windows","minimumCoverage":100,"tests":[{"package":"example/backend/internal/db","name":"TestOwnership"}]}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := readCoveragePolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	platform := policy.platformFiles["internal/db/ownership_windows.go"]
	if policy.floors["internal/gatewayworkspace"] != 7 || policy.defaultFloor != 1 || !policy.neutralPackages["internal/data"] || policy.excludedPackages["cmd/e2e"].Context != "linux-e2e" || platform.Platform != "windows" || platform.BuildConstraint != "windows" || platform.MinimumCoverage != 100 {
		t.Fatalf("unexpected gateway workspace policy: %+v", policy)
	}
}

func TestReadCoverageFloorsRejectsMissingAndInvalidEntries(t *testing.T) {
	for _, content := range []string{
		`{}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"internal/../outside":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"internal/gatewayworkspace":0}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"internal/gatewayworkspace":0.001}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageFloors":{"internal/gatewayworkspace":101}}`,
		`{"backendCoverageDefaultFloor":0,"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageNeutralPackages":["internal/gatewayworkspace"],"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoveragePlatformFiles":{"outside.go":{"platform":"windows","buildConstraint":"windows","minimumCoverage":100,"tests":[{"package":"p","name":"t"}]}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoveragePlatformFiles":{"internal/db/../db/ownership_windows.go":{"platform":"windows","buildConstraint":"windows","minimumCoverage":100,"tests":[{"package":"p","name":"t"}]}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoveragePlatformFiles":{"internal/db/ownership_windows.go":{"platform":"linux","buildConstraint":"linux","minimumCoverage":100,"tests":[]}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoveragePlatformFiles":{"internal/db/ownership_windows.go":{"platform":"windows","buildConstraint":"linux","minimumCoverage":100,"tests":[{"package":"p","name":"t"}]}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoveragePlatformFiles":{"internal/db/ownership_windows.go":{"platform":"windows","buildConstraint":"windows","minimumCoverage":0,"tests":[{"package":"p","name":"t"}]}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageExcludedPackages":{"internal/api":{"context":"linux-e2e","reason":"test"}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageExcludedPackages":{"cmd/e2e":{"context":"host","reason":"test"}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
		`{"backendCoverageDefaultFloor":1,"backendCoverageExcludedPackages":{"cmd/e2e":{"context":"linux-e2e","reason":""}},"backendCoverageFloors":{"internal/gatewayworkspace":7}}`,
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
