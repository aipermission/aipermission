package architecture

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

type backendBuildContext struct {
	name   string
	goos   string
	goarch string
	tags   []string
}

type packageImportGraph struct {
	context backendBuildContext
	imports map[string][]string
}

var supportedBackendBuildContexts = []backendBuildContext{
	{name: "linux", goos: "linux", goarch: runtime.GOARCH},
	{name: "windows", goos: "windows", goarch: runtime.GOARCH},
	{name: "linux-e2e", goos: "linux", goarch: runtime.GOARCH, tags: []string{"e2e"}},
}

var packageImportCache = struct {
	sync.Mutex
	graphs       map[string]map[string][]string
	dependencies map[string]map[string]bool
}{
	graphs:       map[string]map[string][]string{},
	dependencies: map[string]map[string]bool{},
}

func supportedPackageImportGraphs(t *testing.T) []packageImportGraph {
	t.Helper()
	graphs := make([]packageImportGraph, 0, len(supportedBackendBuildContexts))
	for _, buildContext := range supportedBackendBuildContexts {
		graphs = append(graphs, packageImportGraph{
			context: buildContext,
			imports: packageImportsForBuildContext(t, buildContext),
		})
	}
	return graphs
}

// allPackageImports merges direct edges only. Boundary checks should reject an
// edge on either platform; graph properties such as cycles and fan-out must use
// supportedPackageImportGraphs so mutually exclusive files remain isolated.
func allPackageImports(t *testing.T) map[string][]string {
	t.Helper()
	return mergePackageImportGraphs(supportedPackageImportGraphs(t))
}

func mergePackageImportGraphs(graphs []packageImportGraph) map[string][]string {
	merged := map[string]map[string]bool{}
	for _, graph := range graphs {
		for importer, imports := range graph.imports {
			if merged[importer] == nil {
				merged[importer] = map[string]bool{}
			}
			for _, imported := range imports {
				merged[importer][imported] = true
			}
		}
	}
	return flattenImportSets(merged)
}

func packageDependencies(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	merged := map[string]bool{}
	for _, buildContext := range supportedBackendBuildContexts {
		for imported := range packageDependenciesForBuildContext(t, buildContext, pkg) {
			merged[imported] = true
		}
	}
	return merged
}

func packageImportsForBuildContext(t *testing.T, buildContext backendBuildContext) map[string][]string {
	t.Helper()
	cacheKey := buildContext.cacheKey()
	packageImportCache.Lock()
	if cached := packageImportCache.graphs[cacheKey]; cached != nil {
		packageImportCache.Unlock()
		return cached
	}
	packageImportCache.Unlock()

	output := runGoList(t, buildContext, "-f", `{{.ImportPath}}|{{join .Imports " "}}`, "./...")
	sets := map[string]map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		importer, imports, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok || importer == "" {
			continue
		}
		sets[importer] = map[string]bool{}
		for _, imported := range strings.Fields(imports) {
			sets[importer][imported] = true
		}
	}
	result := flattenImportSets(sets)

	packageImportCache.Lock()
	packageImportCache.graphs[cacheKey] = result
	packageImportCache.Unlock()
	return result
}

func packageDependenciesForBuildContext(t *testing.T, buildContext backendBuildContext, pkg string) map[string]bool {
	t.Helper()
	cacheKey := buildContext.cacheKey() + "|" + pkg
	packageImportCache.Lock()
	if cached := packageImportCache.dependencies[cacheKey]; cached != nil {
		packageImportCache.Unlock()
		return cached
	}
	packageImportCache.Unlock()

	output := runGoList(t, buildContext, "-deps", "-f", "{{.ImportPath}}", pkg)
	imports := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != pkg {
			imports[line] = true
		}
	}

	packageImportCache.Lock()
	packageImportCache.dependencies[cacheKey] = imports
	packageImportCache.Unlock()
	return imports
}

func runGoList(t *testing.T, buildContext backendBuildContext, arguments ...string) []byte {
	t.Helper()
	commandArguments := []string{"list", "-buildvcs=false"}
	if len(buildContext.tags) > 0 {
		commandArguments = append(commandArguments, "-tags="+strings.Join(buildContext.tags, ","))
	}
	commandArguments = append(commandArguments, arguments...)
	command := exec.Command("go", commandArguments...)
	command.Dir = "../.."
	command.Env = append(environmentWithoutGoBuildContext(os.Environ()),
		"GOOS="+buildContext.goos,
		"GOARCH="+buildContext.goarch,
		"CGO_ENABLED=1",
	)
	output, err := command.Output()
	if err == nil {
		return output
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		t.Fatalf("go %s failed for %s: %v\n%s", strings.Join(commandArguments, " "), buildContext.name, err, string(exitErr.Stderr))
	}
	t.Fatalf("go %s failed for %s: %v", strings.Join(commandArguments, " "), buildContext.name, err)
	return nil
}

func environmentWithoutGoBuildContext(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, item := range environment {
		name, _, _ := strings.Cut(item, "=")
		switch name {
		case "GOOS", "GOARCH", "CGO_ENABLED":
			continue
		default:
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func flattenImportSets(sets map[string]map[string]bool) map[string][]string {
	result := make(map[string][]string, len(sets))
	for importer, imports := range sets {
		result[importer] = make([]string, 0, len(imports))
		for imported := range imports {
			result[importer] = append(result[importer], imported)
		}
		sort.Strings(result[importer])
	}
	return result
}

func (buildContext backendBuildContext) cacheKey() string {
	return fmt.Sprintf("%s/%s/%s", buildContext.goos, buildContext.goarch, strings.Join(buildContext.tags, ","))
}

func TestSupportedBuildContextsRemainExplicit(t *testing.T) {
	got := make([]string, 0, len(supportedBackendBuildContexts))
	for _, buildContext := range supportedBackendBuildContexts {
		got = append(got, buildContext.name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "linux,linux-e2e,windows" {
		t.Fatalf("architecture guards must cover Linux, Windows, and tagged e2e code; got %v", got)
	}
}

func TestTaggedE2EPackageParticipatesInArchitectureGraph(t *testing.T) {
	const e2ePackage = modulePath + "/cmd/e2e"
	for _, graph := range supportedPackageImportGraphs(t) {
		_, found := graph.imports[e2ePackage]
		if graph.context.name == "linux-e2e" && !found {
			t.Fatal("tagged e2e package is absent from its architecture graph")
		}
		if graph.context.name != "linux-e2e" && found {
			t.Fatalf("tagged e2e package unexpectedly appears in %s graph", graph.context.name)
		}
	}
}

func TestDirectImportChecksMergeEverySupportedBuildContext(t *testing.T) {
	linuxOnly := modulePath + "/internal/linuxonly"
	windowsOnly := modulePath + "/internal/windowsonly"
	merged := mergePackageImportGraphs([]packageImportGraph{
		{context: backendBuildContext{name: "linux"}, imports: map[string][]string{"fixture": {linuxOnly}}},
		{context: backendBuildContext{name: "windows"}, imports: map[string][]string{"fixture": {windowsOnly}}},
	})
	if got := strings.Join(merged["fixture"], ","); got != linuxOnly+","+windowsOnly {
		t.Fatalf("direct import checks did not preserve both platform edges: %s", got)
	}
}
