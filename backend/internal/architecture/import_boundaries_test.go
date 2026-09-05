package architecture

import (
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
)

const modulePath = "github.com/aipermission/aipermission/backend"

func TestConnectorGroundworkImportBoundaries(t *testing.T) {
	packages := []string{
		modulePath + "/internal/connectors",
		modulePath + "/internal/actions",
		modulePath + "/internal/connectortargets",
	}
	forbidden := append([]string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/db",
		modulePath + "/internal/filetransfer",
		modulePath + "/internal/tokens",
		modulePath + "/internal/vault",
	}, builtInConnectorPackages(t)...)

	for _, pkg := range packages {
		imports := packageDependencies(t, pkg)
		for _, forbiddenImport := range forbidden {
			if importsPackageOrSubpackage(imports, forbiddenImport) {
				t.Fatalf("%s must not import %s", pkg, forbiddenImport)
			}
		}
	}
}

func importsPackageOrSubpackage(imports map[string]bool, root string) bool {
	for imported := range imports {
		if imported == root || strings.HasPrefix(imported, root+"/") {
			return true
		}
	}
	return false
}

func TestBuiltInConnectorCorePackagesStayIndependentFromGatewayState(t *testing.T) {
	forbidden := []string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/connectortargets",
		modulePath + "/internal/db",
		modulePath + "/internal/filetransfer",
		modulePath + "/internal/history",
		modulePath + "/internal/tokens",
		modulePath + "/internal/vault",
	}
	for _, pkg := range builtInConnectorPackages(t) {
		t.Run(strings.TrimPrefix(pkg, modulePath+"/internal/connectors/"), func(t *testing.T) {
			imports := packageDependencies(t, pkg)
			for _, forbiddenImport := range forbidden {
				if importsPackageOrSubpackage(imports, forbiddenImport) {
					t.Fatalf("%s must not import gateway state package %s", pkg, forbiddenImport)
				}
			}
		})
	}
}

func TestExtractedDomainPackagesStayIndependentFromAPI(t *testing.T) {
	packages := []string{
		modulePath + "/internal/actionresponse",
		modulePath + "/internal/actions",
		modulePath + "/internal/runtimecontrol",
		modulePath + "/internal/transferjobs",
		modulePath + "/internal/vaultrequests",
	}
	for _, pkg := range packages {
		if importsPackageOrSubpackage(packageDependencies(t, pkg), modulePath+"/internal/api") {
			t.Errorf("%s must not depend on the HTTP/API composition root", pkg)
		}
	}
}

func TestBuiltInConnectorImplementationsStayBehindConnectorBoundary(t *testing.T) {
	allowedRegistry := modulePath + "/internal/connectors/builtin"
	builtInPackages := builtInConnectorPackages(t)
	importsByPackage := allPackageImports(t)
	reviewedTransportEdges := map[string]bool{
		modulePath + "/internal/connectors/docker/apiadapter->" + modulePath + "/internal/connectors/ssh/apiadapter":     true,
		modulePath + "/internal/connectors/kubernetes/apiadapter->" + modulePath + "/internal/connectors/ssh/apiadapter": true,
	}

	for importer, imports := range importsByPackage {
		if importer == allowedRegistry || strings.HasPrefix(importer, allowedRegistry+"/") {
			continue
		}
		for _, imported := range imports {
			importedOwner := builtInConnectorOwner(imported, builtInPackages)
			if importedOwner == "" {
				continue
			}
			importerOwner := builtInConnectorOwner(importer, builtInPackages)
			if importerOwner == importedOwner {
				continue
			}
			if reviewedTransportEdges[importer+"->"+imported] {
				continue
			}
			t.Fatalf("%s imports connector implementation %s; shared runtime and sibling connectors must use the generic connector boundary", importer, imported)
		}
	}
}

func TestConnectorPackagesDoNotImportGatewayState(t *testing.T) {
	connectorRoot := modulePath + "/internal/connectors/"
	allowedVaultOwner := modulePath + "/internal/connectors/ssh/sshkeys"
	forbidden := []string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/db",
		modulePath + "/internal/history",
		modulePath + "/internal/tokens",
	}
	for importer, imports := range allPackageImports(t) {
		if !strings.HasPrefix(importer, connectorRoot) {
			continue
		}
		for _, imported := range imports {
			for _, forbiddenImport := range forbidden {
				if imported == forbiddenImport || strings.HasPrefix(imported, forbiddenImport+"/") {
					t.Fatalf("%s directly imports gateway state %s", importer, imported)
				}
			}
			if (imported == modulePath+"/internal/vault" || strings.HasPrefix(imported, modulePath+"/internal/vault/")) && importer != allowedVaultOwner {
				t.Fatalf("%s directly imports Vault state %s; encrypted resource ownership belongs in %s", importer, imported, allowedVaultOwner)
			}
		}
	}
}

func builtInConnectorOwner(pkg string, builtInPackages []string) string {
	for _, connectorPackage := range builtInPackages {
		if pkg == connectorPackage || strings.HasPrefix(pkg, connectorPackage+"/") {
			return connectorPackage
		}
	}
	return ""
}

func TestInternalPackageFanOutBudgets(t *testing.T) {
	importsByPackage := allPackageImports(t)
	const defaultBudget = 8
	overrides := map[string]int{
		// Composition roots are explicit exceptions. These ceilings match the
		// post-decomposition graph and must ratchet down after dependencies move.
		modulePath + "/internal/api":                       28,
		modulePath + "/internal/connectors/builtin":        16,
		modulePath + "/internal/connectors/ssh/apiadapter": 14,
	}
	for importer, imports := range importsByPackage {
		if !strings.HasPrefix(importer, modulePath+"/internal/") {
			continue
		}
		count := 0
		for _, imported := range imports {
			if strings.HasPrefix(imported, modulePath+"/internal/") {
				count++
			}
		}
		budget := defaultBudget
		if override, ok := overrides[importer]; ok {
			budget = override
		}
		if count > budget {
			t.Errorf("%s has %d direct internal dependencies; fan-out budget is %d", importer, count, budget)
		}
	}
}

func TestInternalDependencyGraphIsAcyclic(t *testing.T) {
	importsByPackage := allPackageImports(t)
	state := map[string]uint8{}
	stack := []string{}
	var visit func(string)
	visit = func(pkg string) {
		switch state[pkg] {
		case 1:
			cycleStart := 0
			for index, item := range stack {
				if item == pkg {
					cycleStart = index
					break
				}
			}
			t.Fatalf("internal dependency cycle: %s", strings.Join(append(stack[cycleStart:], pkg), " -> "))
		case 2:
			return
		}
		state[pkg] = 1
		stack = append(stack, pkg)
		for _, imported := range importsByPackage[pkg] {
			if strings.HasPrefix(imported, modulePath+"/internal/") {
				visit(imported)
			}
		}
		stack = stack[:len(stack)-1]
		state[pkg] = 2
	}
	for pkg := range importsByPackage {
		if strings.HasPrefix(pkg, modulePath+"/internal/") {
			visit(pkg)
		}
	}
}

func builtInConnectorPackages(t *testing.T) []string {
	t.Helper()
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatalf("build connector catalog: %v", err)
	}
	packages := make([]string, 0, len(registry.List()))
	for _, info := range registry.List() {
		packages = append(packages, modulePath+"/internal/connectors/"+info.Kind)
	}
	sort.Strings(packages)
	return packages
}

func allPackageImports(t *testing.T) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-buildvcs=false", "-f", `{{.ImportPath}}|{{join .Imports " "}}`, "./...")
	cmd.Dir = "../.."
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list ./... failed: %v\n%s", err, string(exitErr.Stderr))
		}
		t.Fatalf("go list ./... failed: %v", err)
	}
	result := map[string][]string{}
	for _, line := range strings.Split(string(output), "\n") {
		importer, imports, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok || importer == "" {
			continue
		}
		result[importer] = strings.Fields(imports)
	}
	return result
}

func packageDependencies(t *testing.T, pkg string) map[string]bool {
	t.Helper()

	cmd := exec.Command("go", "list", "-buildvcs=false", "-deps", "-f", "{{.ImportPath}}", pkg)
	cmd.Dir = "../.."
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list %s failed: %v\n%s", pkg, err, string(exitErr.Stderr))
		}
		t.Fatalf("go list %s failed: %v", pkg, err)
	}

	imports := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == pkg {
			continue
		}
		imports[line] = true
	}
	return imports
}
